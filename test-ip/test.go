// tcp_realip_server.go
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
	log.Println("支持长连接")
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
	defer conn.Close()

	// 记录连接时间
	startTime := time.Now()

	// 1. 获取真实IP
	realIP, isProxy, bufferedData := parseProxyProtocol(conn)
	remoteAddr := conn.RemoteAddr().String()

	// 打印连接信息
	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("📡 新连接建立\n")
	fmt.Printf("   连接地址: %s\n", remoteAddr)
	fmt.Printf("   真实IP:   %s\n", realIP)
	fmt.Printf("   代理协议: %v\n", isProxy)
	fmt.Printf("   连接时间: %s\n", startTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("   缓冲数据: %d 字节\n", len(bufferedData))
	fmt.Printf(strings.Repeat("-", 60) + "\n")

	// 发送欢迎消息
	welcomeMsg := fmt.Sprintf(
		"📡 TCP真实IP测试服务器\n"+
			"   连接地址: %s\n"+
			"   真实IP: %s\n"+
			"   代理协议: %v\n"+
			"   输入 'exit' 断开连接\n"+
			strings.Repeat("-", 40)+"\n",
		remoteAddr, realIP, isProxy)
	conn.Write([]byte(welcomeMsg))

	// 如果有缓冲数据，处理它
	if len(bufferedData) > 0 {
		processData(conn, realIP, bufferedData, true)
	}

	// 2. 循环接收数据
	buffer := make([]byte, 4096)
	for {
		// 设置读取超时（可选）
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		n, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				log.Printf("客户端 %s 主动断开连接", realIP)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("连接 %s 读取超时", realIP)
			} else {
				log.Printf("读取错误 %s: %v", realIP, err)
			}
			break
		}

		if n > 0 {
			data := buffer[:n]
			processData(conn, realIP, data, false)
		}
	}

	// 3. 连接关闭
	duration := time.Since(startTime)
	fmt.Printf(strings.Repeat("-", 60) + "\n")
	fmt.Printf("🔌 连接关闭\n")
	fmt.Printf("   真实IP: %s\n", realIP)
	fmt.Printf("   连接时长: %v\n", duration)
	fmt.Printf("   总接收数据: %d 次\n", connectionStats[realIP])
	fmt.Printf(strings.Repeat("=", 60) + "\n\n")
}

// 统计每个IP的连接数据
var connectionStats = make(map[string]int)

func processData(conn net.Conn, realIP string, data []byte, isBuffered bool) {
	// 更新统计
	connectionStats[realIP]++

	// 处理消息
	msg := strings.TrimSpace(string(data))
	dataType := "实时数据"
	if isBuffered {
		dataType = "缓冲数据"
	}

	// 打印日志
	fmt.Printf("📥 收到数据 [%s]\n", dataType)
	fmt.Printf("   真实IP: %s\n", realIP)
	fmt.Printf("   数据长度: %d 字节\n", len(data))
	fmt.Printf("   内容: %s\n", msg)
	if len(data) <= 100 {
		fmt.Printf("   十六进制: %x\n", data)
	}
	fmt.Printf(strings.Repeat("-", 30) + "\n")

	// 处理特殊命令
	switch msg {
	case "exit", "quit":
		conn.Write([]byte("Goodbye! 连接即将关闭...\n"))
		conn.Close()
		return
	case "stats":
		stats := fmt.Sprintf("📊 统计信息\n   真实IP: %s\n   接收次数: %d\n",
			realIP, connectionStats[realIP])
		conn.Write([]byte(stats))
		return
	case "help":
		help := "📖 可用命令:\n" +
			"   exit/quit - 断开连接\n" +
			"   stats     - 查看统计\n" +
			"   help      - 显示帮助\n" +
			"   其他任何消息会被回显\n"
		conn.Write([]byte(help))
		return
	}

	// 回显消息
	echoMsg := fmt.Sprintf("[服务器回显] 真实IP: %s, 你的消息: %s\n", realIP, msg)
	conn.Write([]byte(echoMsg))
}

// 解析代理协议，返回真实IP、是否代理协议、缓冲数据
func parseProxyProtocol(conn net.Conn) (string, bool, []byte) {
	// 代理协议v2签名
	signature := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

	// 尝试读取签名
	buf := make([]byte, 12)
	n, err := conn.Read(buf)
	if err != nil || n < 12 {
		// 不是代理协议或读取失败
		if n > 0 {
			return conn.RemoteAddr().String(), false, buf[:n]
		}
		return conn.RemoteAddr().String(), false, nil
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
		// 不是代理协议，返回已读取的数据作为缓冲数据
		return conn.RemoteAddr().String(), false, buf[:n]
	}

	// 读取版本和命令
	verCmd := make([]byte, 2)
	n2, _ := conn.Read(verCmd)

	// 读取地址信息
	addrInfo := make([]byte, 3)
	n3, _ := conn.Read(addrInfo)

	// 合并已读取的数据（用于调试）
	allReadData := append(buf[:n], append(verCmd[:n2], addrInfo[:n3]...)...)

	// 解析地址长度
	addrLen := binary.BigEndian.Uint16(addrInfo[1:3])

	// 读取地址数据
	if addrLen > 0 {
		addrData := make([]byte, addrLen)
		n4, _ := conn.Read(addrData)

		// 更新已读取的数据
		allReadData = append(allReadData, addrData[:n4]...)

		// 解析真实IP（只处理TCP IPv4）
		addrFamily := addrInfo[0] >> 4
		transport := addrInfo[0] & 0x0F

		if addrFamily == 0x01 && transport == 0x01 && addrLen >= 12 {
			// TCP over IPv4
			srcIP := net.IPv4(addrData[0], addrData[1], addrData[2], addrData[3])
			srcPort := binary.BigEndian.Uint16(addrData[8:10])

			fmt.Printf("🔍 代理协议解析成功:\n")
			fmt.Printf("   源IP: %s:%d\n", srcIP, srcPort)
			fmt.Printf("   目标IP: %d.%d.%d.%d\n",
				addrData[4], addrData[5], addrData[6], addrData[7])
			fmt.Printf("   目标端口: %d\n", binary.BigEndian.Uint16(addrData[10:12]))

			return srcIP.String(), true, nil
		}
	}

	// 其他情况（IPv6、UNIX等）
	return conn.RemoteAddr().String(), true, allReadData
}
