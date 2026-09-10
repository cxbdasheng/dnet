// Echo backend for the Docker smoke check. TCP and UDP share a numeric port.
package main

import (
	"io"
	"log"
	"net"
)

func main() {
	udp, err := net.ListenPacket("udp4", ":19001")
	if err != nil {
		log.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", ":19001")
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := udp.ReadFrom(buf)
			if err != nil {
				log.Fatal(err)
			}
			if _, err := udp.WriteTo(buf[:n], addr); err != nil {
				log.Fatal(err)
			}
		}
	}()
	for {
		conn, err := tcp.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			defer conn.Close()
			_, _ = io.Copy(conn, conn)
		}()
	}
}
