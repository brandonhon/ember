// Package sysinfo detects the host's compute resources and recommends an
// Ollama model size that fits. Used by the `ember probe` subcommand and the
// admin /api/admin/llm endpoint.
package sysinfo

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// SystemInfo is what the probe returns. Numbers are zero when undetectable.
type SystemInfo struct {
	// RAMBytes is the total physical RAM in bytes (0 when unknown).
	RAMBytes uint64 `json:"ram_bytes"`
	// CPUs is the logical CPU count.
	CPUs int `json:"cpus"`
	// GPU is "nvidia", "apple", "amd", or "" when no accelerator detected.
	GPU string `json:"gpu"`
	// OS is runtime.GOOS for callers that want to render the source.
	OS string `json:"os"`
}

// Detect probes the host. Best-effort: missing data is silently zero.
func Detect() SystemInfo {
	return SystemInfo{
		RAMBytes: detectRAM(),
		CPUs:     runtime.NumCPU(),
		GPU:      detectGPU(),
		OS:       runtime.GOOS,
	}
}

// detectRAM reads /proc/meminfo (Linux containers and bare metal). On macOS
// dev environments this returns 0; the recommender treats that as "unknown"
// and picks a conservative default.
func detectRAM() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }() // read-only file; close error is not actionable
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kib, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kib * 1024
	}
	return 0
}

// detectGPU returns the accelerator family if one is reachable. Inside a
// docker container without device passthrough this is normally "".
func detectGPU() string {
	// NVIDIA: device node or binary in PATH.
	if _, err := os.Stat("/dev/nvidia0"); err == nil {
		return "nvidia"
	}
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		return "nvidia"
	}
	// Apple Silicon (only visible on bare-metal darwin/arm64).
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "apple"
	}
	// AMD render node.
	if _, err := os.Stat("/dev/kfd"); err == nil {
		return "amd"
	}
	return ""
}

// Recommendation is the suggested model for the detected system plus a one-
// line explanation.
type Recommendation struct {
	Model      string `json:"model"`
	Reason     string `json:"reason"`
	DisableLLM bool   `json:"disable_llm"`
}

// Recommend picks an Ollama model based on the detected system. Ranking:
//   - any GPU              → qwen2.5:7b
//   - >=16 GiB RAM         → qwen2.5:3b
//   - >=8 GiB RAM          → qwen2.5:1.5b
//   - >=4 GiB RAM          → qwen2.5:0.5b
//   - <4 GiB or unknown    → qwen2.5:0.5b (conservative)
//   - <2 GiB               → disable summaries entirely
func Recommend(s SystemInfo) Recommendation {
	const (
		gib = 1024 * 1024 * 1024
	)
	if s.GPU != "" {
		return Recommendation{
			Model:  "qwen2.5:7b",
			Reason: "GPU detected (" + s.GPU + ") — large model fits comfortably",
		}
	}
	switch {
	case s.RAMBytes >= 16*gib:
		return Recommendation{Model: "qwen2.5:3b", Reason: "16 GiB+ RAM, CPU-only — 3b model fits"}
	case s.RAMBytes >= 8*gib:
		return Recommendation{Model: "qwen2.5:1.5b", Reason: "8 GiB+ RAM, CPU-only — 1.5b model"}
	case s.RAMBytes >= 4*gib:
		return Recommendation{Model: "qwen2.5:0.5b", Reason: "4 GiB+ RAM, CPU-only — 0.5b model for fast summaries"}
	case s.RAMBytes == 0:
		return Recommendation{Model: "qwen2.5:0.5b", Reason: "RAM undetectable — conservative default"}
	case s.RAMBytes < 2*gib:
		return Recommendation{Model: "", DisableLLM: true, Reason: "<2 GiB RAM — disable summaries"}
	default:
		return Recommendation{Model: "qwen2.5:0.5b", Reason: "<4 GiB RAM — smallest model"}
	}
}
