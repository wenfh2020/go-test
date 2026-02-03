// fixed_demo_server.go
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

func main() {
	listener, err := net.Listen("tcp", ":32623")
	if err != nil {
		log.Fatal("监听失败:", err)
	}
	defer listener.Close()

	log.Println("TCP代理协议测试服务器启动:32623")
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

	// 解析代理协议获取真实IP
	realIP, isProxy, bufferData := parseProxyProtocol(conn)

	// 提取纯IP（去掉端口）
	cleanIP := extractIP(realIP)

	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("📡 新连接建立\n")
	fmt.Printf("   连接地址: %s\n", conn.RemoteAddr())
	fmt.Printf("   真实IP:   %s\n", cleanIP)
	fmt.Printf("   代理协议: %v\n", isProxy)
	fmt.Printf("   缓冲数据: %d 字节\n", len(bufferData))

	if len(bufferData) > 0 {
		fmt.Printf("   缓冲数据(hex): %x\n", bufferData)
		fmt.Printf("   缓冲数据(ascii): %q\n", string(bufferData))
	}
	fmt.Printf(strings.Repeat("-", 60) + "\n")

	// 发送欢迎消息
	welcome := fmt.Sprintf("欢迎! 连接地址: %s\n真实IP: %s\n代理协议: %v\n\n",
		conn.RemoteAddr(), cleanIP, isProxy)
	conn.Write([]byte(welcome))

	// 如果有缓冲数据，先处理
	if len(bufferData) > 0 {
		fmt.Printf("处理缓冲数据: %q\n", string(bufferData))
		conn.Write([]byte(fmt.Sprintf("缓冲数据: %q\n", string(bufferData))))
	}

	// 循环读取数据
	buf := make([]byte, 1024)
	for {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				fmt.Printf("客户端断开: %s\n", cleanIP)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("读取超时: %s\n", cleanIP)
			} else {
				fmt.Printf("读取错误: %v\n", err)
			}
			break
		}

		if n > 0 {
			data := buf[:n]
			fmt.Printf("收到数据[%s]: %q (hex: %x)\n", cleanIP, string(data), data)
			conn.Write([]byte(fmt.Sprintf("回显: %s", data)))
		}
	}
}

// parseProxyProtocol 正确解析代理协议
func parseProxyProtocol(conn net.Conn) (realIP string, isProxy bool, bufferData []byte) {
	// 代理协议v2签名
	proxySignature := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

	// 重要：使用 io.ReadAtLeast 确保读取足够的数据
	buf := make([]byte, 16) // 先读16字节查看

	// 设置短超时，避免阻塞
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := io.ReadAtLeast(conn, buf, 12) // 至少读12字节检查签名
	conn.SetReadDeadline(time.Time{})       // 清除超时

	if err != nil {
		// 读取失败，可能不是代理协议
		if n > 0 {
			return conn.RemoteAddr().String(), false, buf[:n]
		}
		return conn.RemoteAddr().String(), false, nil
	}

	// 检查签名
	if n >= 12 {
		// 打印前12字节用于调试
		fmt.Printf("前12字节(hex): ")
		for i := 0; i < 12 && i < n; i++ {
			fmt.Printf("%02x ", buf[i])
		}
		fmt.Println()

		// 比较签名
		match := true
		for i := 0; i < 12; i++ {
			if buf[i] != proxySignature[i] {
				match = false
				break
			}
		}

		if match {
			fmt.Println("✅ 检测到代理协议v2签名")

			// 继续读取完整的代理协议头
			// 头部结构: 12签名 + 2版本/命令 + 2地址长度 + 地址数据

			// 如果已经读了16字节，但还需要更多数据
			if n < 16 {
				// 读取剩下的2字节（版本/命令之后的部分）
				remaining := make([]byte, 16-n)
				_, err := io.ReadFull(conn, remaining)
				if err != nil {
					return conn.RemoteAddr().String(), true, buf[:n]
				}
				buf = append(buf[:n], remaining...)
				n = 16
			}

			// 现在应该有至少16字节
			if n >= 16 {
				// 解析地址长度（在位置14-15）
				addrLen := binary.BigEndian.Uint16(buf[14:16])
				totalHeaderLen := 16 + int(addrLen)

				fmt.Printf("地址长度: %d, 总头部长度: %d\n", addrLen, totalHeaderLen)

				// 读取完整的头部
				header := make([]byte, totalHeaderLen)
				copy(header, buf[:n])

				// 读取剩余部分
				for n < totalHeaderLen {
					readMore, err := conn.Read(header[n:totalHeaderLen])
					if err != nil {
						fmt.Printf("读取代理协议头错误: %v\n", err)
						return conn.RemoteAddr().String(), true, header[:n]
					}
					n += readMore
				}

				// 解析真实IP
				realIP := parseProxyHeader(header)
				if realIP != "" {
					fmt.Printf("✅ 成功解析真实IP: %s\n", realIP)
					return realIP, true, nil
				}
			}

			return conn.RemoteAddr().String(), true, nil
		} else {
			fmt.Println("❌ 不是代理协议签名")
			// 不是代理协议，返回已读取的数据
			return conn.RemoteAddr().String(), false, buf[:n]
		}
	}

	// 数据不足12字节
	return conn.RemoteAddr().String(), false, buf[:n]
}

// parseProxyHeader 解析代理协议头
func parseProxyHeader(data []byte) string {
	if len(data) < 16 {
		return ""
	}

	// 检查版本/命令（位置12）
	verCmd := data[12]
	version := verCmd >> 4
	command := verCmd & 0x0F

	fmt.Printf("版本: 0x%X, 命令: 0x%X\n", version, command)

	if version != 0x02 {
		// 不是代理协议v2
		return ""
	}

	if command == 0x00 {
		// LOCAL命令，没有地址信息
		return ""
	}

	// 解析地址长度
	addrLen := binary.BigEndian.Uint16(data[14:16])

	if len(data) < 16+int(addrLen) {
		return ""
	}

	// 解析地址族和协议（位置13）
	addrFamily := data[13] >> 4
	transport := data[13] & 0x0F

	fmt.Printf("地址族: 0x%X, 传输协议: 0x%X\n", addrFamily, transport)

	addrData := data[16 : 16+addrLen]

	if addrFamily == 0x01 && transport == 0x01 && addrLen >= 12 {
		// TCP over IPv4
		// 注意：AWS NLB 发送的格式是：源IP、目标IP、源端口、目标端口
		// 源IP: 0-3字节，目标IP: 4-7字节，源端口: 8-9字节，目标端口: 10-11字节
		srcIP := net.IPv4(addrData[0], addrData[1], addrData[2], addrData[3])
		srcPort := binary.BigEndian.Uint16(addrData[8:10])
		dstIP := net.IPv4(addrData[4], addrData[5], addrData[6], addrData[7])
		dstPort := binary.BigEndian.Uint16(addrData[10:12])

		fmt.Printf("  源IP: %s:%d\n", srcIP, srcPort)
		fmt.Printf("  目标IP: %s:%d\n", dstIP, dstPort)

		return fmt.Sprintf("%s:%d", srcIP, srcPort)
	}

	if addrFamily == 0x02 && transport == 0x01 && addrLen >= 36 {
		// TCP over IPv6
		srcIP := net.IP(addrData[0:16])
		srcPort := binary.BigEndian.Uint16(addrData[32:34])

		fmt.Printf("  源IP: [%s]:%d\n", srcIP, srcPort)

		return fmt.Sprintf("%s:%d", srcIP, srcPort)
	}

	return ""
}

// extractIP 提取纯IP（去掉端口）
func extractIP(addr string) string {
	// 如果包含冒号，尝试分割IP和端口
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		possibleIP := addr[:idx]
		// 检查是否为合法IP
		if ip := net.ParseIP(possibleIP); ip != nil {
			return ip.String()
		}
	}

	// 如果本身是IP，直接返回
	if ip := net.ParseIP(addr); ip != nil {
		return ip.String()
	}

	return addr
}
