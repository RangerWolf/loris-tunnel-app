package model

// TrafficStats holds real-time upload/download throughput in bytes per second.
type TrafficStats struct {
	UpBps   int64 `json:"upBps"`
	DownBps int64 `json:"downBps"`
}
