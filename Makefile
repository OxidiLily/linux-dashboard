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
# tsc/vite dipanggil langsung, BUKAN `npm run build`: wrapper npm ikut hidup
# sebagai proses node ~60-100 MB selama render dan mendorong puncak build UI
# melewati 512 MB (terukur 2026-10-29). Jangan dikembalikan ke npm run build.
ui:
	cd $(UI) && npm ci && (npm run audit || { i=0; while [ $$i -lt 3 ]; do npm audit fix; npm run audit && break; i=$$((i+1)); done; if [ $$i -eq 3 ]; then echo "WARN: vulnerability tetap ada setelah 3x fix, lanjut build"; fi; }) && ./node_modules/.bin/tsc -b && ./node_modules/.bin/vite build

# NODE_OPTIONS: batasi heap V8 saat build UI supaya muat di perangkat 512 MB.
# Vite 7 (rollup) semua memori di heap V8 yang terikat batas ini; override
# dengan export NODE_OPTIONS bila perangkat lega. ponytail: 320 MiB adalah
# titik kerja terkecil yang teruji 3/3 lolos di cgroup 512 MB tanpa swap.
NODE_OPTIONS ?= --max-old-space-size=320
export NODE_OPTIONS

# ponytail: anggaran per proses, bukan batas RSS; build lebih lambat demi VPS 1 GB.
# Paket SQLite generated besar; batasi compiler tanpa mematikan optimasi runtime.
# GOMEMLIMIT 192MiB (bukan 256MiB): dengan 256MiB, compile/link go pernah
# OOM di cgroup 512 MB — 192MiB teruji lolos penuh di sana (2026-10-29).
GO_BUILD := env GOMAXPROCS=1 GOGC=20 GOMEMLIMIT=192MiB go build -p=1 -gcflags='modernc.org/sqlite/lib=-c=1'
.NOTPARALLEL: build server helper release-server

server:
	CGO_ENABLED=0 $(GO_BUILD) -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-server ./cmd/server

helper: internal/helper/embed/9router.service internal/helper/embed/headroom.service
	CGO_ENABLED=1 $(GO_BUILD) -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-helper ./cmd/helper

# Salin unit 9router ke lokasi //go:embed kalau berubah — sumber kebenaran
# tetap deploy/9router.service, salinannya di-include ke binary helper
# lewat internal/helper/embed/embed.go.
internal/helper/embed/9router.service: deploy/9router.service
	cp $< $@

# Unit headroom punya jembatan pid file (ExecStartPost) yang ikut dikelola di
# sini — kalau embed-nya basi, fix EADDRINUSE di aitools.go tidak pernah
# masuk ke binary helper. Sumber kebenaran tetap deploy/headroom.service.
internal/helper/embed/headroom.service: deploy/headroom.service
	cp $< $@

build: ui server helper

# go test -timeout 30m: tiap test API membuat SQLite di t.TempDir(), dan di
# filesystem dengan fsync lambat (loop device, jaringan) satu store.Open bisa
# ~3 detik — ratusan test internal/api melewati timeout bawaan go test
# (10 menit). TMPDIR sengaja TIDAK dipindah ke /tmp: test seed AI dan mailcow
# memvalidasi kepemilikan/mode ancestor path fixture-nya dan menolak TMPDIR
# world-writable seperti /tmp (mode 1777).
test:
	python3 -B -m unittest deploy/test_postfix.py
	python3 -B -m unittest deploy/test_build_memory.py
	go test -timeout 30m ./...
	python3 -B -m unittest discover -s internal/helper -p 'test_grounded_search.py'
	cd $(UI) && npm run test --if-present

lint:
	go vet ./...

# Cross-compile web app untuk semua arsitektur target sekaligus.
release-server: ui
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO_BUILD) -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-server-linux-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO_BUILD) -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-server-linux-arm64 ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO_BUILD) -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux-dashboard-server-linux-armhf ./cmd/server

dev:
	@echo "Jalankan di tiga terminal:"
	@echo "  sudo go run ./cmd/helper"
	@echo "  go run ./cmd/server"
	@echo "  cd $(UI) && npm run dev"

install: build
	./deploy/install.sh

clean:
	rm -rf $(BINDIR) web/dist/assets
