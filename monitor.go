package main

import (
	"bufio"
	//"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/mem"
	//"github.com/shirou/gopsutil/net"
)

type NetworkStats struct {
	InterfaceName    string `json:"inftname"`
	ReceivedBytes    uint64 `json:"download"`
	TransmittedBytes uint64 `json:"upload"`
}

type DiskStats struct {
	DeviceName string `json:"devname"`
	ReadSpeed  uint64 `json:"rd_speed"`
	WriteSpeed uint64 `json:"wr_speed"`
}

type SystemResourceData struct {
	OSType        string         `json:"os"`
	CPUNum        uint16         `json:"cpu_num"`
	CPUUsage      float64        `json:"cpu_usage"`
	MemoryUsage   float64        `json:"memory_usage"`
	DiskUsage     float64        `json:"disk_usage"`
	Timestamp     string         `json:"time"`
	TotalMemoryMB float64        `json:"total_memory"` //MB
	TotalDiskGB   float64        `json:"total_disk"`   //GB
	IPAddress     string         `json:"ip_address"`
	Network       []NetworkStats `json:"network"`
	Disks         []DiskStats    `json:"disk_io"`
}

func parseUint64(s string) uint64 {
	value, _ := strconv.ParseUint(s, 10, 64)
	return value
}

func formatFloat(value float64) float64 {
	return float64(int(value*100)) / 100
}

func getLocalIPAddress() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String(), nil
				}
			}
		}
	}

	return "", fmt.Errorf("No IP address found")
}

func getNetworkStats() []NetworkStats {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		fmt.Println("Error opening /proc/net/dev:", err)
		return nil
	}
	defer file.Close()

	var stats []NetworkStats
	ignoreInterfaces := regexp.MustCompile(`^(vir|docker|br|lo|veth)`) // Adjust the regex pattern as needed

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, ":") {
			fields := strings.Fields(line)
			interfaceName := strings.TrimSuffix(strings.TrimPrefix(fields[0], ":"), ":")
			if !ignoreInterfaces.MatchString(interfaceName) {
				receivedBytes := parseUint64(fields[1])
				transmittedBytes := parseUint64(fields[9])
				stats = append(stats, NetworkStats{InterfaceName: interfaceName, ReceivedBytes: receivedBytes, TransmittedBytes: transmittedBytes})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading /proc/net/dev:", err)
	}

	return stats
}

func calculateNetworkSpeeds(initialStats, finalStats []NetworkStats) []NetworkStats {
	var networkSpeeds []NetworkStats

	for i := range initialStats {
		receivedBytesDiff := finalStats[i].ReceivedBytes - initialStats[i].ReceivedBytes
		transmittedBytesDiff := finalStats[i].TransmittedBytes - initialStats[i].TransmittedBytes

		networkSpeeds = append(networkSpeeds, NetworkStats{
			InterfaceName:    initialStats[i].InterfaceName,
			ReceivedBytes:    receivedBytesDiff,
			TransmittedBytes: transmittedBytesDiff,
		})
	}

	return networkSpeeds
}

func getDiskStats() []DiskStats {
	file, err := os.Open("/proc/diskstats")
	if err != nil {
		fmt.Println("Error opening /proc/diskstats:", err)
		return nil
	}
	defer file.Close()

	var stats []DiskStats
	ignoreInterfaces := regexp.MustCompile(`^(loop|sr|fd)`) // 屏蔽无关项

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, " ") {
			fields := strings.Fields(line)
			deviceName := strings.TrimSuffix(strings.TrimPrefix(fields[2], ":"), ":")
			if !ignoreInterfaces.MatchString(deviceName) {
				readbytes := parseUint64(fields[5])
				writebytes := parseUint64(fields[9])
				stats = append(stats, DiskStats{DeviceName: deviceName, ReadSpeed: readbytes, WriteSpeed: writebytes})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading /proc/diskstats:", err)
	}
	return stats
}

// 计算磁盘读写
func calculateDiskSpeeds(initialStats, finalStats []DiskStats) []DiskStats {
	var diskSpeeds []DiskStats

	for i := range initialStats {
		readDiff := finalStats[i].ReadSpeed - initialStats[i].ReadSpeed
		writeDiff := finalStats[i].WriteSpeed - initialStats[i].WriteSpeed

		diskSpeeds = append(diskSpeeds, DiskStats{
			DeviceName: finalStats[i].DeviceName,
			ReadSpeed:  readDiff,
			WriteSpeed: writeDiff,
		})
	}

	return diskSpeeds
}

func main() {

	//收集系统信息
	memInfo, _ := mem.VirtualMemory()
	totalDisk, _ := disk.Usage("/")
	ipAddress, _ := getLocalIPAddress()

	// Create a timer to periodically collect and push data
	ticker := time.NewTicker(5 * time.Second)

	// 长连接
	serverAddr := "192.168.31.172:8089"
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Println("Error connecting to server:", err)
		return
	}
	defer conn.Close()

	for {
		select {
		case <-ticker.C:
			// Collect system resource data
			cpuUsage, _ := cpu.Percent(time.Second, false)
			memory, _ := mem.VirtualMemory()
			osType := runtime.GOOS
			cpuNum := runtime.NumCPU()

			initialNetworkStats := getNetworkStats()
			initialDiskStats := getDiskStats()
			time.Sleep(1 * time.Second)
			finalNetworkStats := getNetworkStats()
			finalDiskStats := getDiskStats()

			networkSpeeds := calculateNetworkSpeeds(initialNetworkStats, finalNetworkStats)
			diskSpeeds := calculateDiskSpeeds(initialDiskStats, finalDiskStats)

			timestamp := time.Now().Unix()
			timeStr := time.Unix(timestamp, 0).Format("2006-01-02 15:04:05")
			// Create a JSON object with the collected data
			resourceData := SystemResourceData{
				OSType:        osType,
				CPUNum:        uint16(cpuNum),
				CPUUsage:      formatFloat(cpuUsage[0]),
				MemoryUsage:   formatFloat(memory.UsedPercent),
				DiskUsage:     formatFloat(float64(totalDisk.UsedPercent)),
				Timestamp:     timeStr,
				TotalMemoryMB: formatFloat(float64(memInfo.Total) / 1000 / 1024),          // Convert to MB
				TotalDiskGB:   formatFloat(float64(totalDisk.Total) / 1000 / 1024 / 1024), // Convert to GB
				IPAddress:     ipAddress,
				Network:       networkSpeeds,
				Disks:         diskSpeeds,
			}

			// Convert data to JSON
			jsonData, err := json.Marshal(resourceData)
			if err != nil {
				log.Println("Error encoding JSON:", err)
				continue
			}

			// Print JSON data
			fmt.Println("JSON Data:", string(jsonData))

			// Send data to the server
			// Send JSON data to the server
			_, err = conn.Write(jsonData)
			if err != nil {
				fmt.Println("Error sending data to server:", err)
				continue
			}
		}
	}
}
