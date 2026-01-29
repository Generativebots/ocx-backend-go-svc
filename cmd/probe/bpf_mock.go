package main

// This file is a placeholder for the auto-generated code from bpf2go.
// In a real build, 'go generate' would produce this file.
// We include it here so the 'main.go' compiles via static analysis mock.

import (
	"github.com/cilium/ebpf"
)

type interceptorObjects struct {
	interceptorPrograms
	interceptorMaps
}

func (o *interceptorObjects) Close() error {
	return nil // Mock
}

type interceptorPrograms struct {
	KprobeSysRead    *ebpf.Program `ebpf:"kprobe_sys_read"`
	KretprobeSysRead *ebpf.Program `ebpf:"kretprobe_sys_read"`
	EnforcePolicy    *ebpf.Program `ebpf:"enforce_policy"`
	HandleExit       *ebpf.Program `ebpf:"handle_exit"`
}

type interceptorMaps struct {
	Events         *ebpf.Map `ebpf:"events"`
	ExitEvents     *ebpf.Map `ebpf:"exit_events"`
	PidsToTrace    *ebpf.Map `ebpf:"pids_to_trace"`
	TrackedBuffers *ebpf.Map `ebpf:"tracked_buffers"`
	VerdictCache   *ebpf.Map `ebpf:"verdict_cache"`
}

func loadInterceptorObjects(_ interface{}, _ *ebpf.CollectionOptions) error {
	// Mock successful load
	return nil
}
