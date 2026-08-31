BINDIR   := bin
UI       := web/ui
LDFLAGS  := -s -w

# Web app tidak butuh cgo → bisa cross-compile ke arsitektur mana pun tanpa
# toolchain tambahan. Helper daemon memakai PAM (cgo), jadi harus dibangun
# dengan compiler untuk arsitektur target.
.PHONY: all ui server helper build clean test lint dev install

all: build

# NPM_CONFIG_UPDATE_NOTIFIER=false: npm menyisipkan blok "New major version
# of npm available!" di tengah log build begitu versinya tertinggal. Di sini
# itu murni derau — build tidak peduli versi npm-nya, dan blok itu mudah
# tertukar dengan error yang sebenarnya. Pembaruan npm ditangani installer
# (pastikan_npm di deploy/install.sh), bukan oleh pesan di log.
ui: export NPM_CONFIG_UPDATE_NOTIFIER=false
ui:
	cd $(UI) && npm ci && npm run build
	@# Audit setelah build: moderate dibiarkan lewat (hanya noise), high/critical
	@# ditandai [vuln] di log supaya jelas di modal Update tanpa membatalkan build.
	@if cd $(UI) && ! npm audit --omit=dev --audit-level=high >/tmp/lindash-audit.log 2>&1; then \
		echo "[vuln] Vulnerability high+ terdeteksi:"; \
		cat /tmp/lindash-audit.log; \
		echo "[vuln] Jalankan 'cd web/ui && npm audit fix' (atau --force untuk major bump)."; \
	fi

server:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-server ./cmd/server

helper: internal/helper/embed/9router.service
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-helper ./cmd/helper

# Salin unit 9router ke lokasi //go:embed kalau berubah — sumber kebenaran
# tetap deploy/9router.service, salinannya di-include ke binary helper
# lewat internal/helper/embed/embed.go.
internal/helper/embed/9router.service: deploy/9router.service
	cp $< $@

build: ui server helper

test:
	go test ./...
	cd $(UI) && npm run test --if-present

lint:
	go vet ./...

# Cross-compile web app untuk semua arsitektur target sekaligus.
release-server: ui
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-server-linux-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-server-linux-arm64 ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-server-linux-armhf ./cmd/server

dev:
	@echo "Jalankan di tiga terminal:"
	@echo "  sudo go run ./cmd/helper"
	@echo "  go run ./cmd/server"
	@echo "  cd $(UI) && npm run dev"

install: build
	./deploy/install.sh

clean:
	rm -rf $(BINDIR) web/dist/assets
