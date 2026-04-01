.PHONY: all build-coredns build-subnet-manager build-dnsdist-cert-sync build-cert-publisher \
        install install-coredns install-subnet-manager install-dnsdist-cert-sync install-cert-publisher \
        uninstall uninstall-coredns uninstall-subnet-manager uninstall-dnsdist-cert-sync uninstall-cert-publisher \
        start stop restart status \
        coredns-multi-enable coredns-multi-start coredns-multi-stop coredns-multi-restart coredns-multi-status coredns-multi-switch \
        docker-up docker-down clean tidy

COREDNS_BIN    := bin/coredns-ecs
SUBNET_MGR_BIN := bin/subnet-manager
DNSDIST_CERT_SYNC_BIN := bin/dnsdist-cert-sync
CERT_PUBLISHER_BIN := bin/cert-publisher
COREDNS_INSTANCES ?= instance1 instance2

all: build-coredns build-subnet-manager build-dnsdist-cert-sync build-cert-publisher

build-coredns:
	cd coredns && go build -o ../$(COREDNS_BIN) .
	@echo "Built $(COREDNS_BIN)"

build-subnet-manager:
	cd subnet-manager && go build -o ../$(SUBNET_MGR_BIN) .
	@echo "Built $(SUBNET_MGR_BIN)"

build-dnsdist-cert-sync:
	cd dnsdist-cert-sync && go build -o ../$(DNSDIST_CERT_SYNC_BIN) .
	@echo "Built $(DNSDIST_CERT_SYNC_BIN)"

build-cert-publisher:
	cd cert-publisher && go build -o ../$(CERT_PUBLISHER_BIN) .
	@echo "Built $(CERT_PUBLISHER_BIN)"

tidy:
	cd coredns && go mod tidy
	cd subnet-manager && go mod tidy
	cd dnsdist-cert-sync && go mod tidy
	cd cert-publisher && go mod tidy

# ── 安装 CoreDNS ────────────────────────────────────────────
install-coredns:
	install -m 755 $(COREDNS_BIN) /usr/local/bin/coredns-ecs
	install -d -m 755 /etc/coredns-ecs
	install -d -m 755 /var/lib/coredns-ecs/ip2region
	if [ ! -f /var/lib/coredns-ecs/ip2region/ip2region_v4.xdb ] && [ -f subnet-manager/ip2region_v4.xdb ]; then \
		install -m 644 subnet-manager/ip2region_v4.xdb /var/lib/coredns-ecs/ip2region/ip2region_v4.xdb; \
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
	install -m 755 $(SUBNET_MGR_BIN) /usr/local/bin/subnet-manager
	install -d -m 755 /etc/subnet-manager
	install -d -m 755 /var/lib/subnet-manager/ip2region
	[ -f /etc/subnet-manager/config.yaml ] || \
	    install -m 644 subnet-manager/config.prod.yaml /etc/subnet-manager/config.yaml
	[ -f /etc/subnet-manager/env ] || \
	    install -m 600 /dev/null /etc/subnet-manager/env
	install -m 644 subnet-manager/subnet-manager.service /etc/systemd/system/subnet-manager.service
	systemctl daemon-reload
	systemctl enable subnet-manager
	@echo "subnet-manager 安装完成，请确认 /etc/subnet-manager/config.yaml 后执行: systemctl start subnet-manager"

# ── 安装 dnsdist-cert-sync ──────────────────────────────────
install-dnsdist-cert-sync:
	install -m 755 $(DNSDIST_CERT_SYNC_BIN) /usr/local/bin/dnsdist-cert-sync
	install -d -m 755 /etc/dnsdist-cert-sync
	[ -f /etc/dnsdist-cert-sync/config.yaml ] || \
	    install -m 644 dnsdist-cert-sync/config.prod.yaml /etc/dnsdist-cert-sync/config.yaml
	[ -f /etc/dnsdist-cert-sync/env ] || \
	    install -m 600 /dev/null /etc/dnsdist-cert-sync/env
	install -m 644 dnsdist-cert-sync/dnsdist-cert-sync.service /etc/systemd/system/dnsdist-cert-sync.service
	systemctl daemon-reload
	systemctl enable dnsdist-cert-sync
	@echo "dnsdist-cert-sync 安装完成，请确认 /etc/dnsdist-cert-sync/config.yaml 和 env 后执行: systemctl start dnsdist-cert-sync"

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
install: install-coredns install-subnet-manager install-dnsdist-cert-sync install-cert-publisher

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

uninstall-dnsdist-cert-sync:
	systemctl stop dnsdist-cert-sync 2>/dev/null || true
	systemctl disable dnsdist-cert-sync 2>/dev/null || true
	rm -f /etc/systemd/system/dnsdist-cert-sync.service /usr/local/bin/dnsdist-cert-sync
	systemctl daemon-reload

uninstall-cert-publisher:
	systemctl stop cert-publisher 2>/dev/null || true
	systemctl disable cert-publisher 2>/dev/null || true
	rm -f /etc/systemd/system/cert-publisher.service /usr/local/bin/cert-publisher
	systemctl daemon-reload

uninstall: uninstall-coredns uninstall-subnet-manager uninstall-dnsdist-cert-sync uninstall-cert-publisher

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
