# Makefile for eBPF CO-RE programs
# Builds eBPF programs with BTF support for kernel portability

CLANG ?= clang
LLVM_STRIP ?= llvm-strip
BPFTOOL ?= bpftool
GO ?= go

# Architecture
ARCH := $(shell uname -m | sed 's/x86_64/x86/' | sed 's/aarch64/arm64/')

# Directories
EBPF_DIR := ebpf
OUTPUT_DIR := $(EBPF_DIR)/output
VMLINUX_DIR := $(EBPF_DIR)/vmlinux

# BTF file (from running kernel)
BTF_FILE := /sys/kernel/btf/vmlinux

# eBPF programs
EBPF_PROGS := tls_uprobe_core identity_map_core

# Output files
EBPF_OBJS := $(patsubst %,$(OUTPUT_DIR)/%.bpf.o,$(EBPF_PROGS))
EBPF_SKELS := $(patsubst %,$(OUTPUT_DIR)/%_bpfel.go,$(EBPF_PROGS))

# Compiler flags
CFLAGS := -g -O2 -target bpf -D__TARGET_ARCH_$(ARCH)
INCLUDES := -I$(EBPF_DIR) -I$(VMLINUX_DIR)

.PHONY: all clean vmlinux generate

all: $(EBPF_OBJS) $(EBPF_SKELS)

# Create output directory
$(OUTPUT_DIR):
	mkdir -p $(OUTPUT_DIR)

# Generate vmlinux.h from running kernel
vmlinux: $(VMLINUX_DIR)/vmlinux.h

$(VMLINUX_DIR)/vmlinux.h:
	@echo "Generating vmlinux.h from kernel BTF..."
	@mkdir -p $(VMLINUX_DIR)
	@if [ -f $(BTF_FILE) ]; then \
		$(BPFTOOL) btf dump file $(BTF_FILE) format c > $@; \
	else \
		echo "Error: BTF not available. Kernel must be compiled with CONFIG_DEBUG_INFO_BTF=y"; \
		exit 1; \
	fi

# Compile eBPF programs
$(OUTPUT_DIR)/%.bpf.o: $(EBPF_DIR)/%.bpf.c $(VMLINUX_DIR)/vmlinux.h | $(OUTPUT_DIR)
	@echo "Compiling $<..."
	$(CLANG) $(CFLAGS) $(INCLUDES) -c $< -o $@
	$(LLVM_STRIP) -g $@

# Generate Go bindings with bpf2go
$(OUTPUT_DIR)/%_bpfel.go: $(OUTPUT_DIR)/%.bpf.o
	@echo "Generating Go bindings for $<..."
	cd $(OUTPUT_DIR) && $(GO) run github.com/cilium/ebpf/cmd/bpf2go \
		-target bpfel \
		-type tls_event \
		-type identity_event \
		$(basename $(notdir $<)) \
		$(notdir $<)

# Generate all Go bindings
generate: $(EBPF_SKELS)

# Clean build artifacts
clean:
	rm -rf $(OUTPUT_DIR)
	rm -f $(VMLINUX_DIR)/vmlinux.h

# Test kernel compatibility
test-kernel:
	@echo "Testing kernel compatibility..."
	@echo "Kernel version: $$(uname -r)"
	@if [ -f $(BTF_FILE) ]; then \
		echo "✅ BTF available"; \
	else \
		echo "❌ BTF not available - kernel must be 5.10+"; \
		exit 1; \
	fi
	@echo "✅ Kernel is CO-RE compatible"

# Install dependencies
deps:
	@echo "Installing build dependencies..."
	@which $(CLANG) > /dev/null || (echo "Installing clang..." && sudo apt-get install -y clang llvm)
	@which $(BPFTOOL) > /dev/null || (echo "Installing bpftool..." && sudo apt-get install -y linux-tools-common linux-tools-generic)
	@$(GO) install github.com/cilium/ebpf/cmd/bpf2go@latest

# Help
help:
	@echo "eBPF CO-RE Build System"
	@echo ""
	@echo "Targets:"
	@echo "  all          - Build all eBPF programs and generate Go bindings"
	@echo "  vmlinux      - Generate vmlinux.h from running kernel"
	@echo "  generate     - Generate Go bindings from compiled eBPF"
	@echo "  clean        - Remove build artifacts"
	@echo "  test-kernel  - Test if kernel supports CO-RE"
	@echo "  deps         - Install build dependencies"
	@echo "  help         - Show this help message"
	@echo ""
	@echo "Requirements:"
	@echo "  - Linux kernel 5.10+ with CONFIG_DEBUG_INFO_BTF=y"
	@echo "  - clang/llvm 10+"
	@echo "  - bpftool"
	@echo "  - Go 1.18+"
