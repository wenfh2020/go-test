// simple_realip_test.go
package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
)

func main() {
	listener, err := net.Listen("tcp", ":23334")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("简单真实IP测试服务器启动:23334")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("接受连接失败:", err)
			continue
		}

		go func(c net.Conn) {
			defer c.Close()

			// 尝试解析代理协议
			realIP := getRealIP(c)

			fmt.Printf("\n=== 连接信息 ===\n")
			fmt.Printf("连接地址: %s\n", c.RemoteAddr())
			fmt.Printf("真实IP:   %s\n", realIP)
			fmt.Printf("===============\n")

			// 返回信息给客户端
			response := fmt.Sprintf("Hello! Your real IP is: %s\n", realIP)
			c.Write([]byte(response))
			c.Close()
		}(conn)
	}
}

func getRealIP(conn net.Conn) string {
	// 代理协议v2签名
	signature := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

	// 读取前12字节
	buf := make([]byte, 12)
	n, err := conn.Read(buf)
	if err != nil || n < 12 {
		return conn.RemoteAddr().String()
	}

	// 检查签名
	for i := 0; i < 12; i++ {
		if buf[i] != signature[i] {
			return conn.RemoteAddr().String()
		}
	}

	// 继续读取后续数据
	verCmd := make([]byte, 2)
	conn.Read(verCmd)

	addrInfo := make([]byte, 3)
	conn.Read(addrInfo)

	addrLen := binary.BigEndian.Uint16(addrInfo[1:3])
	if addrLen >= 12 {
		addrData := make([]byte, addrLen)
		conn.Read(addrData)

		// 返回源IP
		return net.IPv4(addrData[0], addrData[1], addrData[2], addrData[3]).String()
	}

	return conn.RemoteAddr().String()
}
