package probe

import (
	"fmt"

	"github.com/cilium/ebpf"
)

const (
	VerdictAllow uint32 = 1
	VerdictBlock uint32 = 2
)

type VerdictUpdater struct {
	verdictMap *ebpf.Map
}

func NewVerdictUpdater(m *ebpf.Map) *VerdictUpdater {
	return &VerdictUpdater{verdictMap: m}
}

// ReleaseProcess tells the kernel that the speculative turn passed validation
func (vu *VerdictUpdater) ReleaseProcess(pid uint32) error {
	// We update the kernel map. The next time the process makes a syscall,
	// the eBPF LSM hook will see 'ALLOW' in the map and let it pass.

	// Note: The C code uses 'verdict_cache' (u32 key -> u32 value)
	err := vu.verdictMap.Update(pid, VerdictAllow, ebpf.UpdateAny)
	if err != nil {
		return fmt.Errorf("failed to update kernel verdict (ALLOW): %w", err)
	}
	return nil
}

// RevokeProcess explicitly blocks a PID
func (vu *VerdictUpdater) RevokeProcess(pid uint32) error {
	return vu.verdictMap.Update(pid, VerdictBlock, ebpf.UpdateAny)
}
