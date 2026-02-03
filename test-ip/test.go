// tcp_realip_server_fixed.go
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
	listener, err := net.Listen("tcp", ":23334")
	if err != nil {
		log.Fatal("监听失败:", err)
	}
	defer listener.Close()

	log.Println("TCP真实IP测试服务器启动，端口: 23334")
	log.Println("支持代理协议 v2")
	log.Println("=================================")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("接受连接失败:", err)
			continue
		}

		go handleClient(conn)
	}
}

func handleClient(conn net.Conn) {
	defer func() {
		conn.Close()
		fmt.Printf("连接关闭: %s\n", conn.RemoteAddr())
	}()

	// 记录连接时间
	startTime := time.Now()

	// 1. 解析代理协议
	realIP, isProxy := parseProxyProtocolWithTimeout(conn, 2*time.Second)
	remoteAddr := conn.RemoteAddr().String()

	// 提取IP（去掉端口）
	displayIP := extractIP(realIP)

	// 打印连接信息
	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("📡 新连接建立\n")
	fmt.Printf("   连接地址: %s\n", remoteAddr)
	fmt.Printf("   真实IP:   %s\n", displayIP)
	fmt.Printf("   代理协议: %v\n", isProxy)
	fmt.Printf("   连接时间: %s\n", startTime.Format("2006-01-02 15:04:05"))
	fmt.Printf(strings.Repeat("-", 60) + "\n")

	// 发送欢迎消息
	welcomeMsg := fmt.Sprintf(
		"TCP真实IP测试服务器\n"+
			"连接地址: %s\n"+
			"真实IP: %s\n"+
			"代理协议: %v\n\n",
		remoteAddr, displayIP, isProxy)
	conn.Write([]byte(welcomeMsg))

	// 2. 循环接收数据
	buffer := make([]byte, 4096)
	for {
		// 设置读取超时
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		n, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				fmt.Printf("客户端主动断开: %s\n", displayIP)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("读取超时: %s\n", displayIP)
			} else {
				fmt.Printf("读取错误 %s: %v\n", displayIP, err)
			}
			break
		}

		if n > 0 {
			data := buffer[:n]
			processData(conn, displayIP, data)
		}
	}

	// 3. 连接关闭统计
	duration := time.Since(startTime)
	fmt.Printf(strings.Repeat("-", 60) + "\n")
	fmt.Printf("🔌 连接关闭\n")
	fmt.Printf("   真实IP: %s\n", displayIP)
	fmt.Printf("   连接时长: %v\n", duration)
	fmt.Printf(strings.Repeat("=", 60) + "\n\n")
}

func processData(conn net.Conn, displayIP string, data []byte) {
	// 处理消息
	msg := strings.TrimSpace(string(data))

	// 打印日志
	fmt.Printf("📥 收到数据\n")
	fmt.Printf("   真实IP: %s\n", displayIP)
	fmt.Printf("   数据长度: %d 字节\n", len(data))
	fmt.Printf("   内容: %q\n", msg)

	if len(data) <= 20 {
		fmt.Printf("   十六进制: %x\n", data)
	}
	fmt.Printf(strings.Repeat("-", 30) + "\n")

	// 处理特殊命令
	if msg == "exit" || msg == "quit" {
		conn.Write([]byte("Goodbye!\n"))
		conn.Close()
		return
	}

	// 简单回显
	conn.Write([]byte(fmt.Sprintf("Echo: %s\n", msg)))
}

// 提取IP（去掉端口）
func extractIP(addr string) string {
	// 如果包含冒号，尝试分割IP和端口
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		possibleIP := addr[:idx]
		// 简单检查是否为IP地址
		if net.ParseIP(possibleIP) != nil {
			return possibleIP
		}
	}
	return addr
}

// 带超时的代理协议解析
func parseProxyProtocolWithTimeout(conn net.Conn, timeout time.Duration) (string, bool) {
	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(timeout))
	defer conn.SetReadDeadline(time.Time{}) // 清除超时

	return parseProxyProtocol(conn)
}

// 解析代理协议
func parseProxyProtocol(conn net.Conn) (string, bool) {
	// 代理协议v2签名
	signature := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

	// 尝试读取签名
	buf := make([]byte, 12)
	n, err := conn.Read(buf)
	if err != nil {
		// 读取错误，返回连接地址
		return conn.RemoteAddr().String(), false
	}

	// 如果读取的数据少于12字节，可能不是代理协议
	if n < 12 {
		// 保存已读取的数据（供后续处理）
		if n > 0 {
			// 这里简化处理：忽略缓冲数据
		}
		return conn.RemoteAddr().String(), false
	}

	// 检查签名
	isProxyV2 := true
	for i := 0; i < 12; i++ {
		if buf[i] != signature[i] {
			isProxyV2 = false
			break
		}
	}

	if !isProxyV2 {
		// 不是代理协议
		return conn.RemoteAddr().String(), false
	}

	// 读取版本和命令
	verCmd := make([]byte, 2)
	if _, err := conn.Read(verCmd); err != nil {
		return conn.RemoteAddr().String(), false
	}

	// 读取地址信息
	addrInfo := make([]byte, 3)
	if _, err := conn.Read(addrInfo); err != nil {
		return conn.RemoteAddr().String(), false
	}

	// 解析地址长度
	addrLen := binary.BigEndian.Uint16(addrInfo[1:3])

	// 读取地址数据
	if addrLen > 0 {
		addrData := make([]byte, addrLen)
		if _, err := conn.Read(addrData); err != nil {
			return conn.RemoteAddr().String(), false
		}

		// 解析地址族和协议
		addrFamily := addrInfo[0] >> 4
		transport := addrInfo[0] & 0x0F

		if addrFamily == 0x01 && transport == 0x01 && addrLen >= 12 {
			// TCP over IPv4
			srcIP := net.IPv4(addrData[0], addrData[1], addrData[2], addrData[3])
			srcPort := binary.BigEndian.Uint16(addrData[8:10])

			fmt.Printf("🔍 代理协议解析成功:\n")
			fmt.Printf("   源IP: %s:%d\n", srcIP, srcPort)

			return fmt.Sprintf("%s:%d", srcIP, srcPort), true
		}

		if addrFamily == 0x02 && transport == 0x01 && addrLen >= 36 {
			// TCP over IPv6
			srcIP := net.IP(addrData[0:16])
			srcPort := binary.BigEndian.Uint16(addrData[32:34])

			fmt.Printf("🔍 代理协议解析成功 (IPv6):\n")
			fmt.Printf("   源IP: [%s]:%d\n", srcIP, srcPort)

			return fmt.Sprintf("%s:%d", srcIP, srcPort), true
		}
	}

	return conn.RemoteAddr().String(), true
}
