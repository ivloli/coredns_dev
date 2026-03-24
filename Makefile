.PHONY: all build-coredns build-subnet-manager \
        install install-coredns install-subnet-manager \
        uninstall uninstall-coredns uninstall-subnet-manager \
        start stop restart status \
        docker-up docker-down clean tidy

COREDNS_BIN    := bin/coredns-ecs
SUBNET_MGR_BIN := bin/subnet-manager

all: build-coredns build-subnet-manager

build-coredns:
	cd coredns && go build -o ../$(COREDNS_BIN) .
	@echo "Built $(COREDNS_BIN)"

build-subnet-manager:
	cd subnet-manager && go build -o ../$(SUBNET_MGR_BIN) .
	@echo "Built $(SUBNET_MGR_BIN)"

tidy:
	cd coredns && go mod tidy
	cd subnet-manager && go mod tidy

# ── 安装 CoreDNS ────────────────────────────────────────────
install-coredns:
	install -m 755 $(COREDNS_BIN) /usr/local/bin/coredns-ecs
	install -d -m 755 /etc/coredns-ecs
	[ -f /etc/coredns-ecs/Corefile ] || \
	    install -m 644 coredns/Corefile.prod /etc/coredns-ecs/Corefile
	install -m 644 coredns/coredns-ecs.service /etc/systemd/system/coredns-ecs.service
	systemctl daemon-reload
	systemctl enable coredns-ecs
	@echo "coredns-ecs 安装完成，请确认 /etc/coredns-ecs/Corefile 后执行: systemctl start coredns-ecs"

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

# ── 一键安装两个服务 ────────────────────────────────────────
install: install-coredns install-subnet-manager

# ── 卸载（不删除配置和数据目录）───────────────────────────
uninstall-coredns:
	systemctl stop coredns-ecs 2>/dev/null || true
	systemctl disable coredns-ecs 2>/dev/null || true
	rm -f /etc/systemd/system/coredns-ecs.service /usr/local/bin/coredns-ecs
	systemctl daemon-reload

uninstall-subnet-manager:
	systemctl stop subnet-manager 2>/dev/null || true
	systemctl disable subnet-manager 2>/dev/null || true
	rm -f /etc/systemd/system/subnet-manager.service /usr/local/bin/subnet-manager
	systemctl daemon-reload

uninstall: uninstall-coredns uninstall-subnet-manager

# ── 服务控制（两个同时操作）────────────────────────────────
start:
	systemctl start subnet-manager coredns

stop:
	systemctl stop coredns subnet-manager

restart:
	systemctl restart coredns subnet-manager

status:
	systemctl status coredns subnet-manager

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down -v

clean:
	rm -rf bin/
