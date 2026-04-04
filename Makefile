.PHONY: all build-coredns build-subnet-manager build-cert-publisher \
        install install-coredns install-subnet-manager install-cert-publisher \
        uninstall uninstall-coredns uninstall-subnet-manager uninstall-cert-publisher \
        start stop restart status \
        coredns-multi-enable coredns-multi-start coredns-multi-stop coredns-multi-restart coredns-multi-status coredns-multi-switch \
        docker-up docker-down clean tidy \
        prepare-prod-dir build-prod-binaries download-prod-ip2region write-prod-version prepare-prod \
        verify-prod-package prod-package prod-install

COREDNS_BIN    := bin/coredns-ecs
SUBNET_MGR_BIN := bin/subnet-manager
CERT_PUBLISHER_BIN := bin/cert-publisher
COREDNS_INSTANCES ?= instance1 instance2
PROD_DEPLOY_DIR ?= production-deploy
PROD_BIN_DIR := $(PROD_DEPLOY_DIR)/bin
PROD_IP2REGION_DIR := $(PROD_DEPLOY_DIR)/ip2region
PROD_COREDNS_BIN := $(PROD_BIN_DIR)/coredns-ecs
PROD_SUBNET_MGR_BIN := $(PROD_BIN_DIR)/subnet-manager
IP2REGION_TXT_NAME := ipv4_source.txt
IP2REGION_XDB_NAME := ip2region_v4.xdb
IP2REGION_VERSION_NAME := .version
COREDNS_IP2REGION_DIR ?= /var/lib/coredns-ecs/ip2region
COREDNS_IP2REGION_XDB_PATH ?= $(COREDNS_IP2REGION_DIR)/$(IP2REGION_XDB_NAME)
SUBNET_MGR_IP2REGION_DIR ?= /var/lib/subnet-manager/ip2region
IP2REGION_TXT_URL ?= https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ipv4_source.txt
IP2REGION_XDB_URL ?= https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v4.xdb
IP2REGION_TXT_MIN_BYTES ?= 1000000
IP2REGION_XDB_MIN_BYTES ?= 1000000
PROD_BUILD_GOOS ?= linux
PROD_BUILD_GOARCH ?= amd64

all: build-coredns build-subnet-manager build-cert-publisher

prepare-prod-dir:
	install -d -m 755 $(PROD_BIN_DIR)
	install -d -m 755 $(PROD_IP2REGION_DIR)

build-prod-binaries: prepare-prod-dir
	cd coredns && GOWORK=off CGO_ENABLED=0 GOOS=$(PROD_BUILD_GOOS) GOARCH=$(PROD_BUILD_GOARCH) go build -o ../$(PROD_COREDNS_BIN) .
	cd subnet-manager && GOWORK=off CGO_ENABLED=0 GOOS=$(PROD_BUILD_GOOS) GOARCH=$(PROD_BUILD_GOARCH) go build -o ../$(PROD_SUBNET_MGR_BIN) .
	@echo "Built production binaries in $(PROD_BIN_DIR)"

download-prod-ip2region: prepare-prod-dir
	@set -e; \
	txt_file="$(PROD_IP2REGION_DIR)/$(IP2REGION_TXT_NAME)"; \
	xdb_file="$(PROD_IP2REGION_DIR)/$(IP2REGION_XDB_NAME)"; \
	if [ -f "$$txt_file" ] && [ $$(wc -c < "$$txt_file") -ge $(IP2REGION_TXT_MIN_BYTES) ]; then \
		echo "Using existing $$txt_file"; \
	else \
		tmp_file="$$txt_file.tmp"; \
		rm -f "$$tmp_file"; \
		curl -fL --retry 3 --retry-delay 2 --connect-timeout 10 --max-time 300 "$(IP2REGION_TXT_URL)" -o "$$tmp_file"; \
		[ $$(wc -c < "$$tmp_file") -ge $(IP2REGION_TXT_MIN_BYTES) ]; \
		mv "$$tmp_file" "$$txt_file"; \
		chmod 644 "$$txt_file"; \
	fi; \
	if [ -f "$$xdb_file" ] && [ $$(wc -c < "$$xdb_file") -ge $(IP2REGION_XDB_MIN_BYTES) ]; then \
		echo "Using existing $$xdb_file"; \
	else \
		tmp_file="$$xdb_file.tmp"; \
		rm -f "$$tmp_file"; \
		curl -fL --retry 3 --retry-delay 2 --connect-timeout 10 --max-time 300 "$(IP2REGION_XDB_URL)" -o "$$tmp_file"; \
		[ $$(wc -c < "$$tmp_file") -ge $(IP2REGION_XDB_MIN_BYTES) ]; \
		mv "$$tmp_file" "$$xdb_file"; \
		chmod 644 "$$xdb_file"; \
	fi
	@echo "Prepared ip2region data in $(PROD_IP2REGION_DIR)"

write-prod-version: prepare-prod-dir
	if [ ! -f $(PROD_IP2REGION_DIR)/$(IP2REGION_VERSION_NAME) ]; then \
		printf 'manual\n' > $(PROD_IP2REGION_DIR)/$(IP2REGION_VERSION_NAME); \
		chmod 644 $(PROD_IP2REGION_DIR)/$(IP2REGION_VERSION_NAME); \
	fi
	@echo "Prepared version file $(PROD_IP2REGION_DIR)/$(IP2REGION_VERSION_NAME)"

prepare-prod: build-prod-binaries download-prod-ip2region write-prod-version
	@echo "Production deploy directory is ready: $(PROD_DEPLOY_DIR)"

verify-prod-package:
	@missing=0; \
	for f in $(PROD_COREDNS_BIN) $(PROD_SUBNET_MGR_BIN) \
		$(PROD_IP2REGION_DIR)/$(IP2REGION_TXT_NAME) \
		$(PROD_IP2REGION_DIR)/$(IP2REGION_XDB_NAME) \
		$(PROD_IP2REGION_DIR)/$(IP2REGION_VERSION_NAME); do \
		if [ ! -f $$f ]; then \
			echo "Missing required production artifact: $$f"; \
			missing=1; \
		fi; \
	done; \
	if [ $$missing -ne 0 ]; then \
		echo "Please run 'make prod-package' first on a build machine."; \
		exit 1; \
	fi
	@[ $$(wc -c < $(PROD_IP2REGION_DIR)/$(IP2REGION_TXT_NAME)) -ge $(IP2REGION_TXT_MIN_BYTES) ] || \
		( echo "Invalid ip2region txt file: $(PROD_IP2REGION_DIR)/$(IP2REGION_TXT_NAME)"; exit 1 )
	@[ $$(wc -c < $(PROD_IP2REGION_DIR)/$(IP2REGION_XDB_NAME)) -ge $(IP2REGION_XDB_MIN_BYTES) ] || \
		( echo "Invalid ip2region xdb file: $(PROD_IP2REGION_DIR)/$(IP2REGION_XDB_NAME)"; exit 1 )
	@echo "Production package verified in $(PROD_DEPLOY_DIR)"

prod-package: prepare-prod
	@echo "Production package prepared in $(PROD_DEPLOY_DIR)"

prod-install: verify-prod-package install-coredns install-subnet-manager
	@echo "Offline install complete (coredns-ecs + subnet-manager)"

build-coredns:
	cd coredns && GOWORK=off go build -o ../$(COREDNS_BIN) .
	@echo "Built $(COREDNS_BIN)"

build-subnet-manager:
	cd subnet-manager && GOWORK=off go build -o ../$(SUBNET_MGR_BIN) .
	@echo "Built $(SUBNET_MGR_BIN)"

build-cert-publisher:
	cd cert-publisher && go build -o ../$(CERT_PUBLISHER_BIN) .
	@echo "Built $(CERT_PUBLISHER_BIN)"

tidy:
	cd coredns && go mod tidy
	cd subnet-manager && go mod tidy
	cd cert-publisher && go mod tidy

# ── 安装 CoreDNS ────────────────────────────────────────────
install-coredns:
	SRC_BIN=$(COREDNS_BIN); \
	if [ -f $(PROD_COREDNS_BIN) ]; then SRC_BIN=$(PROD_COREDNS_BIN); fi; \
	install -m 755 $$SRC_BIN /usr/local/bin/coredns-ecs
	install -d -m 755 /etc/coredns-ecs
	install -d -m 755 $(COREDNS_IP2REGION_DIR)
	if [ -f $(PROD_IP2REGION_DIR)/$(IP2REGION_XDB_NAME) ]; then \
		install -m 644 $(PROD_IP2REGION_DIR)/$(IP2REGION_XDB_NAME) $(COREDNS_IP2REGION_XDB_PATH); \
	elif [ ! -f $(COREDNS_IP2REGION_XDB_PATH) ] && [ -f subnet-manager/ip2region_v4.xdb ]; then \
		install -m 644 subnet-manager/ip2region_v4.xdb $(COREDNS_IP2REGION_XDB_PATH); \
	fi
	[ -f /etc/coredns-ecs/Corefile ] || \
	    install -m 644 coredns/Corefile.prod /etc/coredns-ecs/Corefile
	install -m 644 coredns/coredns-ecs.service /etc/systemd/system/coredns-ecs.service
	install -m 644 coredns/coredns-ecs@.service /etc/systemd/system/coredns-ecs@.service
	for i in $(COREDNS_INSTANCES); do \
		install -d -m 755 /etc/coredns-ecs/$$i; \
		[ -f /etc/coredns-ecs/$$i/Corefile ] || install -m 644 coredns/Corefile.prod /etc/coredns-ecs/$$i/Corefile; \
	done
	systemctl daemon-reload
	systemctl enable coredns-ecs
	@echo "coredns-ecs 安装完成，请确认 /etc/coredns-ecs/Corefile 与 /var/lib/coredns-ecs/ip2region/ip2region_v4.xdb 后执行: systemctl start coredns-ecs"

# ── 安装 subnet-manager ─────────────────────────────────────
install-subnet-manager:
	SRC_BIN=$(SUBNET_MGR_BIN); \
	if [ -f $(PROD_SUBNET_MGR_BIN) ]; then SRC_BIN=$(PROD_SUBNET_MGR_BIN); fi; \
	install -m 755 $$SRC_BIN /usr/local/bin/subnet-manager
	install -d -m 755 /etc/subnet-manager
	install -d -m 755 $(SUBNET_MGR_IP2REGION_DIR)
	if [ -f $(PROD_IP2REGION_DIR)/$(IP2REGION_TXT_NAME) ]; then \
		install -m 644 $(PROD_IP2REGION_DIR)/$(IP2REGION_TXT_NAME) $(SUBNET_MGR_IP2REGION_DIR)/$(IP2REGION_TXT_NAME); \
	fi
	if [ -f $(PROD_IP2REGION_DIR)/$(IP2REGION_XDB_NAME) ]; then \
		install -m 644 $(PROD_IP2REGION_DIR)/$(IP2REGION_XDB_NAME) $(SUBNET_MGR_IP2REGION_DIR)/$(IP2REGION_XDB_NAME); \
	fi
	if [ -f $(PROD_IP2REGION_DIR)/$(IP2REGION_VERSION_NAME) ]; then \
		install -m 644 $(PROD_IP2REGION_DIR)/$(IP2REGION_VERSION_NAME) $(SUBNET_MGR_IP2REGION_DIR)/$(IP2REGION_VERSION_NAME); \
	fi
	[ -f /etc/subnet-manager/config.yaml ] || \
	    install -m 644 subnet-manager/config.prod.yaml /etc/subnet-manager/config.yaml
	[ -f /etc/subnet-manager/env ] || \
	    install -m 600 /dev/null /etc/subnet-manager/env
	install -m 644 subnet-manager/subnet-manager.service /etc/systemd/system/subnet-manager.service
	systemctl daemon-reload
	systemctl enable subnet-manager
	@echo "subnet-manager 安装完成，请确认 /etc/subnet-manager/config.yaml 后执行: systemctl start subnet-manager"

# ── 安装 cert-publisher ───────────────────────────────────
install-cert-publisher:
	install -m 755 $(CERT_PUBLISHER_BIN) /usr/local/bin/cert-publisher
	install -d -m 755 /etc/cert-publisher
	[ -f /etc/cert-publisher/config.yaml ] || \
	    install -m 644 cert-publisher/config.prod.yaml /etc/cert-publisher/config.yaml
	[ -f /etc/cert-publisher/env ] || \
	    install -m 600 /dev/null /etc/cert-publisher/env
	install -m 644 cert-publisher/cert-publisher.service /etc/systemd/system/cert-publisher.service
	systemctl daemon-reload
	systemctl enable cert-publisher
	@echo "cert-publisher 安装完成，请确认 /etc/cert-publisher/config.yaml 和 env 后执行: systemctl start cert-publisher"

# ── 一键安装两个服务 ────────────────────────────────────────
install: install-coredns install-subnet-manager install-cert-publisher

# ── 卸载（不删除配置和数据目录）───────────────────────────
uninstall-coredns:
	systemctl stop coredns-ecs 2>/dev/null || true
	systemctl disable coredns-ecs 2>/dev/null || true
	for i in $(COREDNS_INSTANCES); do systemctl stop coredns-ecs@$$i 2>/dev/null || true; done
	for i in $(COREDNS_INSTANCES); do systemctl disable coredns-ecs@$$i 2>/dev/null || true; done
	rm -f /etc/systemd/system/coredns-ecs.service /etc/systemd/system/coredns-ecs@.service /usr/local/bin/coredns-ecs
	systemctl daemon-reload

uninstall-subnet-manager:
	systemctl stop subnet-manager 2>/dev/null || true
	systemctl disable subnet-manager 2>/dev/null || true
	rm -f /etc/systemd/system/subnet-manager.service /usr/local/bin/subnet-manager
	systemctl daemon-reload

uninstall-cert-publisher:
	systemctl stop cert-publisher 2>/dev/null || true
	systemctl disable cert-publisher 2>/dev/null || true
	rm -f /etc/systemd/system/cert-publisher.service /usr/local/bin/cert-publisher
	systemctl daemon-reload

uninstall: uninstall-coredns uninstall-subnet-manager uninstall-cert-publisher

# ── 服务控制（两个同时操作）────────────────────────────────
start:
	systemctl start subnet-manager coredns-ecs

stop:
	systemctl stop coredns-ecs subnet-manager

restart:
	systemctl restart coredns-ecs subnet-manager

status:
	systemctl status coredns-ecs subnet-manager

# ── CoreDNS 多实例控制（systemd template: coredns-ecs@.service）────
coredns-multi-enable:
	systemctl daemon-reload
	for i in $(COREDNS_INSTANCES); do systemctl enable coredns-ecs@$$i; done
	@echo "Enabled coredns-ecs instances: $(COREDNS_INSTANCES)"

coredns-multi-start:
	for i in $(COREDNS_INSTANCES); do systemctl start coredns-ecs@$$i; done
	@echo "Started coredns-ecs instances: $(COREDNS_INSTANCES)"

coredns-multi-stop:
	for i in $(COREDNS_INSTANCES); do systemctl stop coredns-ecs@$$i; done

coredns-multi-restart:
	for i in $(COREDNS_INSTANCES); do systemctl restart coredns-ecs@$$i; done

coredns-multi-status:
	for i in $(COREDNS_INSTANCES); do systemctl status coredns-ecs@$$i; done

coredns-multi-switch:
	systemctl stop coredns-ecs 2>/dev/null || true
	systemctl disable coredns-ecs 2>/dev/null || true
	for i in $(COREDNS_INSTANCES); do systemctl stop coredns@$$i 2>/dev/null || true; done
	for i in $(COREDNS_INSTANCES); do systemctl disable coredns@$$i 2>/dev/null || true; done
	$(MAKE) coredns-multi-enable
	$(MAKE) coredns-multi-start

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down -v

clean:
	rm -rf bin/
