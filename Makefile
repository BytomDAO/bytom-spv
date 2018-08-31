ifndef GOOS
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
	GOOS := darwin
else ifeq ($(UNAME_S),Linux)
	GOOS := linux
else
$(error "$$GOOS is not defined. If you are using Windows, try to re-make using 'GOOS=windows make ...' ")
endif
endif

PACKAGES    := $(shell go list ./... | grep -v '/vendor/' | grep -v '/crypto/ed25519/chainkd' | grep -v '/mining/tensority')
PACKAGES += 'github.com/bytom-spv/mining/tensority/go_algorithm'

BUILD_FLAGS := -ldflags "-X github.com/bytom-spv/version.GitCommit=`git rev-parse HEAD`"

BYTOM_SPV_BINARY32 := bytom-spv-wallet-$(GOOS)_386
BYTOM_SPV_BINARY64 := bytomd-spv-wallet-$(GOOS)_amd64

VERSION := $(shell awk -F= '/Version =/ {print $$2}' version/version.go | tr -d "\" ")


BYTOM_SPV_RELEASE32 := bytom-$(VERSION)-$(GOOS)_386
BYTOM_SPV_RELEASE64 := bytom-$(VERSION)-$(GOOS)_amd64


all: test target release-all

bytom-spv:
	@echo "Building bytom-spv-wallet to cmd/bytomd/bytom-spv-wallet"
	@go build $(BUILD_FLAGS) -o cmd/bytomd/bytom-spv-wallet cmd/bytomd/main.go

target:
	mkdir -p $@

binary: target/$(BYTOM_SPV_BINARY32) target/$(BYTOM_SPV_BINARY64)

ifeq ($(GOOS),windows)
release: binary
	cd target && cp -f $(BYTOM_SPV_BINARY32) $(BYTOM_SPV_BINARY32).exe
	cd target && md5sum $(BYTOM_SPV_BINARY32).exe >$(BYTOM_SPV_RELEASE32).md5
	cd target && zip $(BYTOM_SPV_RELEASE32).zip $(BYTOM_SPV_BINARY32).exe $(BYTOM_SPV_RELEASE32).md5
	cd target && rm -f  $(BYTOM_SPV_BINARY32) $(BYTOM_SPV_BINARY32).exe $(BYTOM_SPV_RELEASE32).md5
	cd target && cp -f $(BYTOM_SPV_BINARY64) $(BYTOM_SPV_BINARY64).exe
	cd target && md5sum $(BYTOM_SPV_BINARY64).exe >$(BYTOM_SPV_RELEASE64).md5
	cd target && zip $(BYTOM_SPV_RELEASE64).zip  $(BYTOM_SPV_BINARY64).exe $(BYTOM_SPV_RELEASE64).md5
	cd target && rm -f $(BYTOM_SPV_BINARY64)   $(BYTOM_SPV_BINARY64).exe $(BYTOM_SPV_RELEASE64).md5
else
release: binary
	cd target && md5sum  $(BYTOM_SPV_BINARY32)  >$(BYTOM_SPV_RELEASE32).md5
	cd target && tar -czf $(BYTOM_SPV_RELEASE32).tgz  $(BYTOM_SPV_BINARY32)  $(BYTOM_SPV_RELEASE32).md5
	cd target && rm -f  $(BYTOM_SPV_BINARY32)  $(BYTOM_SPV_RELEASE32).md5
	cd target && md5sum  $(BYTOM_SPV_BINARY64)  >$(BYTOM_SPV_RELEASE64).md5
	cd target && tar -czf $(BYTOM_SPV_RELEASE64).tgz  $(BYTOM_SPV_BINARY64)  $(BYTOM_SPV_RELEASE64).md5
	cd target && rm -f  $(BYTOM_SPV_BINARY64)  $(BYTOM_SPV_RELEASE64).md5
endif

release-all: clean
	GOOS=darwin  make release
	GOOS=linux   make release
	GOOS=windows make release

clean:
	@echo "Cleaning binaries built..."
	@rm -rf cmd/bytomd/bytomd
	@rm -rf cmd/bytomcli/bytomcli
	@rm -rf cmd/miner/miner
	@rm -rf target
	@echo "Cleaning temp test data..."
	@rm -rf test/pseudo_hsm*
	@rm -rf blockchain/pseudohsm/testdata/pseudo/
	@echo "Cleaning sm2 pem files..."
	@rm -rf crypto/sm2/*.pem
	@echo "Done."

target/$(BYTOM_SPV_BINARY32):
	CGO_ENABLED=0 GOARCH=386 go build $(BUILD_FLAGS) -o $@ cmd/bytomd/main.go

target/$(BYTOM_SPV_BINARY64):
	CGO_ENABLED=0 GOARCH=amd64 go build $(BUILD_FLAGS) -o $@ cmd/bytomd/main.go

test:
	@echo "====> Running go test"
	@go test -tags "network" $(PACKAGES)

benchmark:
	@go test -bench $(PACKAGES)

functional-tests:
	@go test -timeout=5m -tags="functional" ./test 

ci: test functional-tests

.PHONY: all target release-all clean test benchmark
