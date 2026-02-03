// fixed_proxy_server.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

func main() {
	listener, err := net.Listen("tcp", ":23334")
	if err != nil {
		log.Fatal("监听失败:", err)
	}
	defer listener.Close()

	log.Println("TCP代理协议测试服务器启动:23334")
	log.Println("等待连接...")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("接受连接失败:", err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		fmt.Printf("连接关闭: %s\n\n", conn.RemoteAddr())
	}()

	// 不设置超时，等待代理协议头
	realIP, isProxy, remainingData := detectProxyProtocol(conn)

	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("📡 新连接建立\n")
	fmt.Printf("   连接地址: %s\n", conn.RemoteAddr())
	fmt.Printf("   真实IP:   %s\n", realIP)
	fmt.Printf("   代理协议: %v\n", isProxy)
	fmt.Printf("   剩余数据: %d 字节\n", len(remainingData))

	if len(remainingData) > 0 {
		fmt.Printf("   剩余数据(hex): %x\n", remainingData)
	}
	fmt.Printf(strings.Repeat("-", 60) + "\n")

	// 如果有剩余数据，先处理
	if len(remainingData) > 0 {
		fmt.Printf("处理剩余数据: %q\n", string(remainingData))
		conn.Write([]byte(fmt.Sprintf("收到缓冲数据: %q\n", string(remainingData))))
	}

	// 发送欢迎消息
	welcome := fmt.Sprintf("欢迎! 真实IP: %s, 代理协议: %v\n", realIP, isProxy)
	conn.Write([]byte(welcome))

	// 循环读取数据
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				fmt.Printf("客户端断开: %s\n", realIP)
			} else {
				fmt.Printf("读取错误: %v\n", err)
			}
			break
		}

		if n > 0 {
			data := buf[:n]
			fmt.Printf("收到数据[%s]: %q (hex: %x)\n", realIP, string(data), data)
			conn.Write([]byte(fmt.Sprintf("回显: %s", data)))
		}
	}
}

// 检测代理协议 - 关键修复版本
func detectProxyProtocol(conn net.Conn) (realIP string, isProxy bool, remainingData []byte) {
	// 代理协议v2签名
	proxySignature := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

	// 重要：使用 Peek 的方式读取，而不是直接 Read
	// 因为我们需要先检查数据，但不一定消费

	// 方法1：先读取少量数据检查
	buffer := make([]byte, 16) // 先读16字节
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buffer)
	conn.SetReadDeadline(time.Time{}) // 清除超时

	if err != nil || n < 12 {
		// 读取失败或数据不足
		if n > 0 {
			return conn.RemoteAddr().String(), false, buffer[:n]
		}
		return conn.RemoteAddr().String(), false, nil
	}

	// 检查是否是代理协议
	if n >= 12 && bytes.Equal(buffer[:12], proxySignature) {
		fmt.Println("✅ 检测到代理协议签名!")

		// 继续读取完整头部
		// 头部总长度 = 12(签名) + 2(版本命令) + 2(地址长度) + 地址数据

		// 如果已经读取了16字节，还需要解析地址长度
		if n >= 16 {
			addrLen := binary.BigEndian.Uint16(buffer[14:16])
			totalHeaderLen := 16 + int(addrLen)

			// 读取剩余头部数据
			headerData := make([]byte, totalHeaderLen)
			copy(headerData[:n], buffer[:n])

			// 读取剩余部分
			for n < totalHeaderLen {
				readMore, err := conn.Read(headerData[n:totalHeaderLen])
				if err != nil {
					fmt.Printf("读取代理协议头错误: %v\n", err)
					return conn.RemoteAddr().String(), true, nil
				}
				n += readMore
			}

			// 解析真实IP
			realIP := parseProxyHeader(headerData)
			if realIP != "" {
				return realIP, true, nil
			}
		}

		return conn.RemoteAddr().String(), true, nil
	}

	// 不是代理协议
	return conn.RemoteAddr().String(), false, buffer[:n]
}

func parseProxyHeader(data []byte) string {
	if len(data) < 16 {
		return ""
	}

	addrLen := binary.BigEndian.Uint16(data[14:16])
	if len(data) < 16+int(addrLen) {
		return ""
	}

	addrFamily := data[12] >> 4
	transport := data[12] & 0x0F

	addrData := data[16 : 16+addrLen]

	if addrFamily == 0x01 && transport == 0x01 && addrLen >= 12 {
		// TCP IPv4
		srcIP := net.IPv4(addrData[0], addrData[1], addrData[2], addrData[3])
		srcPort := binary.BigEndian.Uint16(addrData[8:10])

		fmt.Printf("  源IP: %s:%d\n", srcIP, srcPort)
		fmt.Printf("  目标IP: %d.%d.%d.%d:%d\n",
			addrData[4], addrData[5], addrData[6], addrData[7],
			binary.BigEndian.Uint16(addrData[10:12]))

		return srcIP.String()
	}

	return ""
}
