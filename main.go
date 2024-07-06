package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	address := flag.String("listen-address", ":9123", "The address to listen on for HTTP requests.")
	devices, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}
	defaultDevice := "eth0"
	for _, iface := range devices {
		if strings.HasPrefix(iface.Name, "zt") || strings.HasPrefix(iface.Name, "ZeroTier") {
			defaultDevice = iface.Name
			break
		}
	}
	device := flag.String("zerotier-device", defaultDevice, "The zerotier device name")
	flag.Parse()

	var trafficIn = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zerotier_host_received_bytes_total",
			Help: "zerotier_host_received_bytes_total",
		},
		[]string{"addr"},
	)
	var trafficOut = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zerotier_host_sent_bytes_total",
			Help: "zerotier_host_sent_bytes_total",
		},
		[]string{"addr"},
	)
	r := prometheus.NewRegistry()
	r.MustRegister(trafficIn)
	r.MustRegister(trafficOut)

	// 打开网络接口
	// 由于只需要读取 header，所以只需要 128 位数据，zerotier 也只会发送所有数据包的前 128 位过来
	// https://en.wikipedia.org/wiki/IPv4#Header
	handle, err := pcap.OpenLive(*device, 128, true, pcap.BlockForever)
	if err != nil {
		log.Fatal(err)
	}
	defer handle.Close()

	// 使用 gopacket 创建包源
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	// 循环读取数据包
	go func() {
		for packet := range packetSource.Packets() {
			// 获取 IP 层
			ipLayer := packet.Layer(layers.LayerTypeIPv4)
			if ipLayer != nil {
				ip, _ := ipLayer.(*layers.IPv4)
				// 仅统计源地址和目标地址均为内网的数据
				if ip.SrcIP.IsPrivate() && ip.DstIP.IsPrivate() {
					// 由于每份数据包在发送端和接收端都被单独计算了一遍，所以实际数据量需要除以 2
					trafficOut.WithLabelValues(ip.SrcIP.String()).Add(float64(ip.Length / 2))
					trafficIn.WithLabelValues(ip.DstIP.String()).Add(float64(ip.Length / 2))
				}
			}
		}
	}()

	http.Handle("/metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{}))
	log.Printf("listening on %s, zerotier device is: %s", *address, *device)
	log.Fatal(http.ListenAndServe(*address, nil))
}
