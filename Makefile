VM ?= mesh
# Binaries always target Linux: they run in the lab, even when built on macOS.
GOARCH ?= $(shell go env GOARCH)

.PHONY: help build test proto demo demo-symmetric lab lab-symmetric lab-down nat-cone nat-symmetric prove vm vm-shell

help:
	@echo "On macOS:"
	@echo "  make vm              create the Multipass VM '$(VM)', mount this repo, install deps"
	@echo "  make vm-shell        open a shell in the VM"
	@echo "Inside the VM (from the repo dir, as root):"
	@echo "  sudo make lab        build the NAT lab (nat-a cone, nat-b cone)"
	@echo "  sudo make lab-symmetric  same, but nat-b is symmetric"
	@echo "  sudo make nat-cone | nat-symmetric   switch nat-b in place"
	@echo "  sudo make prove      reachability + NAT mapping proof via tcpdump"
	@echo "  sudo make lab-down   remove all lab namespaces"
	@echo "  sudo make demo       coordinator + STUN + two peers; expect a direct punch"
	@echo "  sudo make demo-symmetric   same with nat-b symmetric; expect the punch to fail"
	@echo "Anywhere:"
	@echo "  make build           build Linux binaries into bin/"
	@echo "  make test            unit tests"
	@echo "  make proto           regenerate gRPC code (needs protoc, protoc-gen-go, protoc-gen-go-grpc)"

build:
	GOOS=linux GOARCH=$(GOARCH) CGO_ENABLED=0 go build -o bin/ ./cmd/...

test:
	go test -race ./...

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative proto/minimesh.proto

demo: build
	lab/demo.sh cone

demo-symmetric: build
	lab/demo.sh symmetric

lab:
	lab/up.sh cone

lab-symmetric:
	lab/up.sh symmetric

lab-down:
	lab/down.sh

nat-cone:
	lab/nat-mode.sh cone nat-b

nat-symmetric:
	lab/nat-mode.sh symmetric nat-b

prove:
	lab/prove.sh

vm:
	multipass info $(VM) >/dev/null 2>&1 || multipass launch 24.04 --name $(VM) --cpus 2 --memory 4G --disk 20G
	multipass mount $(CURDIR) $(VM):/home/ubuntu/minimesh 2>/dev/null || true
	multipass exec $(VM) -- sudo /home/ubuntu/minimesh/lab/vm-setup.sh

vm-shell:
	multipass shell $(VM)
