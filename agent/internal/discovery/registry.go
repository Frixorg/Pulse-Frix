package discovery

// DefaultDetectors returns the full set of v1 detectors. Detectors that cannot
// run in the current environment report Available()=false and are skipped
// gracefully, so this list is safe to use everywhere.
func DefaultDetectors() []Detector {
	return []Detector{
		OSDetector{},
		DockerDetector{},
		&DockerStorageDetector{},
		NginxDetector{},
		ProxyDetector{},
		SystemdDetector{},
		InitDDetector{},
		PortDetector{},
		NetworkDetector{},
		FilesystemDetector{},
		ProcessDetector{},
		DatabaseDetector{},
		ApplicationDetector{},
		SSLDetector{},
		MonitoringDetector{},
		SecurityDetector{},
	}
}
