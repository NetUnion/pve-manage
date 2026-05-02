package service

import (
	"cloud-manage/internal/config"
)

type OptionsResponse struct {
	Clusters []ClusterOption `json:"clusters"`
}

type ClusterOption struct {
	Key       string          `json:"key"`
	Name      string          `json:"name"`
	Limit     int             `json:"limit"`
	StartVMID int             `json:"start_vmid"`
	CPU       []CPUOption     `json:"cpu"`
	Storage   []StorageOption `json:"storage"`
	Network   []NetworkOption `json:"network"`
}

type CPUOption struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Limit       int      `json:"limit"`
	MemoryLimit int      `json:"memory_limit"`
	Node        []string `json:"node"`
}

type StorageOption struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Limit int    `json:"limit"`
}

type NetworkOption struct {
	Key   string `json:"key"`
	UESTC string `json:"uestc"`
	IPv4  any    `json:"ipv4"`
	IPv6  any    `json:"ipv6"`
}

func BuildOptions(cfg *config.App) OptionsResponse {
	clusters := make([]ClusterOption, 0, len(cfg.Root.Cluster))
	for clusterKey, cluster := range cfg.Root.Cluster {
		item := ClusterOption{
			Key:       clusterKey,
			Name:      cluster.Name,
			Limit:     cluster.Limit,
			StartVMID: cluster.StartVMID,
			CPU:       make([]CPUOption, 0, len(cluster.CPU)),
			Storage:   make([]StorageOption, 0, len(cluster.Storage)),
			Network:   make([]NetworkOption, 0, len(cluster.Network.Bridge)),
		}

		for cpuKey, cpu := range cluster.CPU {
			item.CPU = append(item.CPU, CPUOption{
				Key:         cpuKey,
				Name:        cpu.Name,
				Limit:       cpu.Limit,
				MemoryLimit: cpu.MemoryLimit,
				Node:        append([]string(nil), cpu.Node...),
			})
		}

		for storageKey, storage := range cluster.Storage {
			item.Storage = append(item.Storage, StorageOption{
				Key:   storageKey,
				Name:  storage.Name,
				Limit: storage.Limit,
			})
		}

		for networkKey, bridge := range cluster.Network.Bridge {
			item.Network = append(item.Network, NetworkOption{
				Key:   networkKey,
				UESTC: cluster.Network.UESTC,
				IPv4:  bridge.IPv4,
				IPv6:  bridge.IPv6,
			})
		}

		clusters = append(clusters, item)
	}

	return OptionsResponse{Clusters: clusters}
}
