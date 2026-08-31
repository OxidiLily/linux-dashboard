package helper

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Print server di atas CUPS.
//
// Semua state dibaca lewat perintah CUPS (lpstat/lpinfo), bukan dengan menulis
// atau membaca /etc/cups/printers.conf langsung — berkas itu milik cupsd, yang
// menulisnya ulang sesuka hati dan mengabaikan perubahan luar sampai
// direstart. Satu-satunya pengecualian adalah pembacaan flag Shared, yang
// memang tidak diekspos lpstat sama sekali.

const cupsPrintersConf = "/etc/cups/printers.conf"

var (
	// CUPS menolak nama antrean yang memuat spasi, "/", atau "#".
	printerNamaRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,127}$`)
	// Nilai -o yang diteruskan ke lp. Sengaja sempit: apa pun di sini masuk ke
	// argumen perintah, jadi bukan tempat untuk teks bebas.
	printerOpsiRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	// Model/PPD dari `lpinfo -m`, mis. "everywhere" atau
	// "drv:///sample.drv/generic.ppd".
	printerModelRe = regexp.MustCompile(`^[A-Za-z0-9_./:%-]{1,255}$`)
	printerURIRe   = regexp.MustCompile(`^[A-Za-z0-9+.-]+://[A-Za-z0-9_.:/?&=%@~+-]{1,255}$`)
	// Baris "printer <nama> is <state>." / "printer <nama> disabled since ...".
	lpstatPrinterRe = regexp.MustCompile(`^printer\s+(\S+)\s+(?:is\s+(\S+?)\.|(disabled))`)
	// Baris `lpstat -o`: "<printer>-<id> <user> <size> <tanggal...>".
	lpstatJobRe = regexp.MustCompile(`^(\S+)-(\d+)\s+(\S+)\s+(\d+)`)
	// Keluaran lp: "request id is Printer-42 (1 file(s))".
	lpRequestRe = regexp.MustCompile(`request id is (\S+)`)
)

// skemaURIDilarang: "file://" membuat cupsd menulis hasil cetak ke berkas
// sembarang di disk sebagai root — itu bukan printer, itu primitif tulis-berkas.
var skemaURIDilarang = map[string]bool{"file": true}

func cupsAda() bool {
	_, ok := lookBinary("lpstat")
	return ok
}

func errCupsBelumAda() error {
	return errKode(helperproto.ErrBelumTerpasang,
		"CUPS belum terpasang — pasang komponen \"print-server\" dulu lewat Settings → Components")
}

// printerList menggabungkan tiga sumber yang masing-masing hanya tahu
// sebagiannya: `lpstat -l -p` (state, enabled, deskripsi, lokasi), `lpstat -v`
// (device URI), dan `lpstat -d` (antrean default).
func printerList() ([]helperproto.Printer, error) {
	if !cupsAda() {
		return []helperproto.Printer{}, nil
	}
	out := []helperproto.Printer{}
	idx := map[string]int{}

	// lpstat keluar dengan status != 0 kalau belum ada satu pun antrean.
	// Itu kondisi normal untuk server yang baru dipasang, bukan kegagalan.
	res, _ := run("lpstat", "-l", "-p")
	var cur *helperproto.Printer
	simpan := func() {
		if cur != nil {
			idx[cur.Name] = len(out)
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if m := lpstatPrinterRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			simpan()
			state := m[2]
			if state == "" {
				state = "stopped"
			}
			cur = &helperproto.Printer{
				Name:  m[1],
				State: state,
				// "disabled since ..." muncul di baris yang sama; tanpa kata itu
				// antrean menerima pekerjaan.
				Enabled: !strings.Contains(line, "disabled since"),
			}
			continue
		}
		if cur == nil {
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "Description:"):
			cur.Description = strings.TrimSpace(strings.TrimPrefix(t, "Description:"))
		case strings.HasPrefix(t, "Location:"):
			cur.Location = strings.TrimSpace(strings.TrimPrefix(t, "Location:"))
		}
	}
	simpan()

	// "device for Foo: socket://10.0.0.5:9100"
	if res, err := run("lpstat", "-v"); err == nil {
		for _, line := range strings.Split(res.Stdout, "\n") {
			sisa, ok := strings.CutPrefix(strings.TrimSpace(line), "device for ")
			if !ok {
				continue
			}
			nama, uri, ok := strings.Cut(sisa, ":")
			if !ok {
				continue
			}
			if i, ada := idx[strings.TrimSpace(nama)]; ada {
				out[i].URI = strings.TrimSpace(uri)
			}
		}
	}

	// "system default destination: Foo"
	if res, err := run("lpstat", "-d"); err == nil {
		if _, nama, ok := strings.Cut(strings.TrimSpace(res.Stdout), ": "); ok {
			if i, ada := idx[strings.TrimSpace(nama)]; ada {
				out[i].Default = true
			}
		}
	}

	for nama := range printerShared() {
		if i, ada := idx[nama]; ada {
			out[i].Shared = true
		}
	}
	return out, nil
}

// printerShared membaca flag Shared dari printers.conf. lpstat tidak
// mengekspos ini sama sekali, padahal justru itu yang menentukan apakah
// printer terlihat oleh klien lain di jaringan — inti dari sebuah print server.
func printerShared() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(cupsPrintersConf)
	if err != nil {
		return out
	}
	nama := ""
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		// "<Printer Foo>" atau "<DefaultPrinter Foo>"
		if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") && strings.Contains(t, "Printer ") {
			_, sisa, _ := strings.Cut(strings.Trim(t, "<>"), " ")
			nama = strings.TrimSpace(sisa)
			continue
		}
		if strings.EqualFold(t, "</Printer>") {
			nama = ""
			continue
		}
		if nama != "" && strings.EqualFold(t, "Shared Yes") {
			out[nama] = true
		}
	}
	return out
}

func printerAdd(a helperproto.PrinterAddArgs) error {
	if !cupsAda() {
		return errCupsBelumAda()
	}
	if !printerNamaRe.MatchString(a.Name) {
		return errInvalid("nama printer tidak valid — huruf/angka/titik/garis, tanpa spasi")
	}
	if !printerURIRe.MatchString(a.URI) {
		return errInvalid("device URI tidak valid")
	}
	skema, _, _ := strings.Cut(a.URI, "://")
	if skemaURIDilarang[strings.ToLower(skema)] {
		return errInvalid("device URI dengan skema %q tidak diizinkan", skema)
	}
	if a.Model != "" && !printerModelRe.MatchString(a.Model) {
		return errInvalid("model/driver tidak valid")
	}
	// Deskripsi dan lokasi adalah teks bebas, tapi tetap lewat argumen
	// perintah — baris baru di dalamnya tidak punya alasan untuk ada.
	for _, v := range []string{a.Description, a.Location} {
		if strings.ContainsAny(v, "\r\n") {
			return errInvalid("deskripsi/lokasi tidak boleh mengandung baris baru")
		}
	}

	args := []string{"-p", a.Name, "-v", a.URI, "-E"}
	if a.Model != "" {
		args = append(args, "-m", a.Model)
	}
	if a.Description != "" {
		args = append(args, "-D", a.Description)
	}
	if a.Location != "" {
		args = append(args, "-L", a.Location)
	}
	args = append(args, "-o", "printer-is-shared="+strconv.FormatBool(a.Shared))
	if _, err := run("lpadmin", args...); err != nil {
		return err
	}
	// -E pada lpadmin hanya berarti "terima pekerjaan"; antreannya sendiri
	// masih perlu dinyalakan supaya benar-benar mencetak.
	_, _ = run("cupsenable", a.Name)
	return nil
}

func printerDelete(nama string) error {
	if !cupsAda() {
		return errCupsBelumAda()
	}
	if !printerNamaRe.MatchString(nama) {
		return errInvalid("nama printer tidak valid")
	}
	_, err := run("lpadmin", "-x", nama)
	return err
}

func printerSetDefault(nama string) error {
	if !cupsAda() {
		return errCupsBelumAda()
	}
	if !printerNamaRe.MatchString(nama) {
		return errInvalid("nama printer tidak valid")
	}
	_, err := run("lpadmin", "-d", nama)
	return err
}

func printerEnable(nama string, enable bool) error {
	if !cupsAda() {
		return errCupsBelumAda()
	}
	if !printerNamaRe.MatchString(nama) {
		return errInvalid("nama printer tidak valid")
	}
	bin := "cupsdisable"
	if enable {
		bin = "cupsenable"
	}
	_, err := run(bin, nama)
	return err
}

// printerDevices menjalankan penemuan perangkat. Batas waktu ditulis eksplisit:
// tanpa itu lpinfo memindai jaringan sampai selesai dan permintaan HTTP di
// baliknya menggantung selama itu.
func printerDevices() ([]helperproto.PrinterDevice, error) {
	if !cupsAda() {
		return []helperproto.PrinterDevice{}, nil
	}
	res, err := run("lpinfo", "--timeout", "8", "-l", "-v")
	if err != nil && res.Stdout == "" {
		return []helperproto.PrinterDevice{}, nil
	}
	out := []helperproto.PrinterDevice{}
	var cur *helperproto.PrinterDevice
	simpan := func() {
		if cur != nil && cur.URI != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		t := strings.TrimSpace(line)
		// Baris pembuka satu perangkat SUDAH membawa uri-nya:
		//   "Device: uri = usb://Canon/iP2700%20series?serial=..."
		// Memperlakukannya sekadar sebagai penanda lalu melompat ke baris
		// berikutnya membuang satu-satunya tempat uri muncul, dan seluruh
		// perangkat ikut terbuang oleh penyaring uri kosong di simpan().
		if sisa, ok := strings.CutPrefix(t, "Device:"); ok {
			simpan()
			cur = &helperproto.PrinterDevice{}
			if k, v, ok := strings.Cut(sisa, "="); ok && strings.TrimSpace(k) == "uri" {
				cur.URI = strings.TrimSpace(v)
			}
			continue
		}
		if cur == nil {
			// Bentuk ringkas `lpinfo -v` tanpa -l: "<kelas> <uri>".
			if _, uri, ok := strings.Cut(t, " "); ok && strings.Contains(uri, "://") {
				out = append(out, helperproto.PrinterDevice{URI: strings.TrimSpace(uri)})
			}
			continue
		}
		key, val, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "uri":
			cur.URI = strings.TrimSpace(val)
		case "info":
			cur.Info = strings.TrimSpace(val)
		case "make-and-model":
			// Lebih deskriptif daripada info pada sebagian backend, dan itulah
			// nama yang paling mendekati tulisan di badan printer.
			if cur.Info == "" {
				cur.Info = strings.TrimSpace(val)
			}
		}
	}
	simpan()
	return out, nil
}

// batasModelPrinter membatasi jumlah PPD yang dikirim ke UI.
//
// ponytail: daftar ini dipotong, bukan dipaginasi atau dicari di server. Sistem
// dengan paket driver lengkap (mis. foomatic) punya puluhan ribu PPD, dan
// mengirim semuanya membuat satu respons JSON berukuran megabyte untuk sebuah
// dropdown. Printer modern memakai "everywhere" (IPP Everywhere) yang selalu
// ada di urutan awal, jadi pemotongan ini praktis tidak terasa. Jalan naiknya:
// terima parameter pencarian dari UI dan saring di sini kalau ada deployment
// yang butuh driver lawas spesifik.
const batasModelPrinter = 500

func printerModels() ([]helperproto.PrinterModel, error) {
	if !cupsAda() {
		return []helperproto.PrinterModel{}, nil
	}
	res, err := run("lpinfo", "-m")
	if err != nil && res.Stdout == "" {
		return []helperproto.PrinterModel{}, nil
	}
	out := []helperproto.PrinterModel{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		model, nama, ok := strings.Cut(t, " ")
		if !ok {
			continue
		}
		out = append(out, helperproto.PrinterModel{Model: model, Name: strings.TrimSpace(nama)})
		if len(out) >= batasModelPrinter {
			break
		}
	}
	return out, nil
}

func printJobs() ([]helperproto.PrintJob, error) {
	if !cupsAda() {
		return []helperproto.PrintJob{}, nil
	}
	// Seperti lpstat -p, status != 0 saat antrean kosong adalah hal normal.
	res, _ := run("lpstat", "-o")
	out := []helperproto.PrintJob{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		m := lpstatJobRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		ukuran, _ := strconv.ParseInt(m[4], 10, 64)
		out = append(out, helperproto.PrintJob{
			ID: m[1] + "-" + m[2], Printer: m[1], User: m[3], Size: ukuran,
		})
	}
	return out, nil
}

func printCancel(id string) error {
	if !cupsAda() {
		return errCupsBelumAda()
	}
	// Bentuk id job sama dengan nama antrean plus "-<angka>".
	if !printerNamaRe.MatchString(id) {
		return errInvalid("id job tidak valid")
	}
	_, err := run("cancel", id)
	return err
}

// printFile mencetak satu berkas milik user.
//
// Berkasnya TIDAK dibuka oleh helper yang berjalan sebagai root: isinya
// dialirkan lewat worker yang privilegenya sudah diturunkan ke user login
// (workerOp "read"), lalu masuk ke stdin lp. Dengan begitu "cetak berkas ini"
// tidak pernah bisa dipakai membaca berkas yang user sendiri tidak berhak
// membacanya — pemeriksaan path saja tidak cukup untuk itu, karena root bisa
// membuka apa pun yang lolos pemeriksaan.
func (s *Server) printFile(u *userInfo, a helperproto.PrintFileArgs) (helperproto.PrintFileHasil, error) {
	if !cupsAda() {
		return helperproto.PrintFileHasil{}, errCupsBelumAda()
	}
	path, err := s.checkPath(u, a.Path)
	if err != nil {
		return helperproto.PrintFileHasil{}, err
	}
	if a.Printer != "" && !printerNamaRe.MatchString(a.Printer) {
		return helperproto.PrintFileHasil{}, errInvalid("nama printer tidak valid")
	}
	if a.Copies < 0 || a.Copies > 100 {
		return helperproto.PrintFileHasil{}, errInvalid("jumlah salinan harus 1–100")
	}
	for _, v := range []string{a.Media, a.Sides} {
		if v != "" && !printerOpsiRe.MatchString(v) {
			return helperproto.PrintFileHasil{}, errInvalid("opsi cetak tidak valid")
		}
	}

	args := []string{}
	if a.Printer != "" {
		args = append(args, "-d", a.Printer)
	}
	if a.Copies > 1 {
		args = append(args, "-n", strconv.Itoa(a.Copies))
	}
	if a.Media != "" {
		args = append(args, "-o", "media="+a.Media)
	}
	if a.Sides != "" {
		args = append(args, "-o", "sides="+a.Sides)
	}
	// -U membuat pekerjaan tercatat atas nama user login, bukan root, sehingga
	// kolom "user" di antrean cocok dengan siapa yang menekan Print.
	args = append(args, "-U", u.Name, "-t", judulCetak(path), "-")

	cmd := exec.Command("lp", args...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return helperproto.PrintFileHasil{}, err
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return helperproto.PrintFileHasil{}, err
	}
	_, errBaca := runAsUser(u, workerOp{Op: "read", Path: path}, nil, stdin)
	stdin.Close()
	errTunggu := cmd.Wait()
	if errBaca != nil {
		return helperproto.PrintFileHasil{}, errBaca
	}
	if errTunggu != nil {
		return helperproto.PrintFileHasil{}, errInvalid("cetak gagal: %s",
			strings.TrimSpace(firstNonEmpty(stderr.String(), errTunggu.Error())))
	}

	hasil := helperproto.PrintFileHasil{Printer: a.Printer}
	if m := lpRequestRe.FindStringSubmatch(stdout.String()); m != nil {
		hasil.JobID = m[1]
		if hasil.Printer == "" {
			if i := strings.LastIndex(m[1], "-"); i > 0 {
				hasil.Printer = m[1][:i]
			}
		}
	}
	return hasil, nil
}

// judulCetak memberi nama pekerjaan supaya antrean bisa dibaca manusia.
// Panjangnya dibatasi karena judul ikut ditampilkan di antrean CUPS.
func judulCetak(path string) string {
	nama := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		nama = path[i+1:]
	}
	nama = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, nama)
	if len(nama) > 120 {
		nama = nama[:120]
	}
	if nama == "" {
		nama = "cetak"
	}
	return nama
}

// ---- deteksi printer & pemasangan driver ----
//
// Bagian ini yang membuat alur di panel selesai sendiri: pasang komponen →
// deteksi printer → pasang driver → daftarkan antrean, tanpa user perlu
// membuka terminal. Tanpa ini, printer USB rumahan tetap terlihat di daftar
// perangkat tapi tidak pernah bisa dipakai, karena PPD-nya memang belum ada
// di sistem dan tidak ada satu pun tombol yang memasangnya.

// paketDriverPrinter memetakan vendor ke paket driver di repo Debian/Ubuntu.
//
// Ini WHITELIST, bukan sekadar tabel bantu: endpoint pemasangan driver hanya
// menerima nama vendor, lalu paketnya diambil dari sini. Dengan begitu jalur
// "panel memasang paket atas permintaan frontend" tidak pernah bisa berubah
// jadi "frontend memilih paket apa pun untuk dipasang sebagai root".
var paketDriverPrinter = map[string][]string{
	// Gutenprint mencakup hampir seluruh PIXMA/Selphy inkjet Canon.
	"canon": {"printer-driver-gutenprint"},
	// hplip membawa hpcups sekaligus utilitas pemindainya.
	"hp":      {"hplip"},
	"epson":   {"printer-driver-escpr"},
	"brother": {"printer-driver-brlaser"},
	"samsung": {"printer-driver-splix"},
	"lexmark": {"printer-driver-foo2zjs"},
	"kyocera": {"printer-driver-foo2zjs"},
	"xerox":   {"printer-driver-foo2zjs"},
	"ricoh":   {"printer-driver-foo2zjs"},
	// Vendor yang tidak dikenali tetap punya peluang lewat Gutenprint, yang
	// juga memuat sejumlah driver generik.
	"generic": {"printer-driver-gutenprint"},
}

// kataUmumModel dibuang sebelum pencocokan driver. "iP2700 series" tidak akan
// pernah cocok dengan PPD bernama "Canon PIXMA iP2700", tapi "ip2700" cocok.
var kataUmumModel = map[string]bool{
	"series": true, "printer": true, "inc": true, "co": true, "ltd": true,
}

// normalisasiModel menyisakan huruf dan angka dalam huruf kecil, supaya
// "iP2700 series", "PIXMA iP2700", dan "ip2700" bisa dibandingkan.
func normalisasiModel(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// kunciCocok memilih potongan nama produk yang layak dipakai mencari PPD:
// token yang memuat angka (mis. "ip2700", "dcp1610") jauh lebih menentukan
// daripada merek atau kata "series".
func kunciCocok(produk string) string {
	terbaik := ""
	for _, tok := range strings.FieldsFunc(produk, func(r rune) bool {
		return r == ' ' || r == '_' || r == '-' || r == '/'
	}) {
		n := normalisasiModel(tok)
		if n == "" || kataUmumModel[n] {
			continue
		}
		if strings.ContainsAny(n, "0123456789") && len(n) > len(terbaik) {
			terbaik = n
		}
	}
	if terbaik == "" {
		terbaik = normalisasiModel(produk)
	}
	return terbaik
}

// vendorProdukDariURI memetik vendor dan nama produk dari device URI.
// Bentuk USB CUPS: "usb://Canon/iP2700%20series?serial=ABC123".
func vendorProdukDariURI(uri, info string) (vendor, produk string) {
	if sisa, ok := strings.CutPrefix(uri, "usb://"); ok {
		sisa, _, _ = strings.Cut(sisa, "?")
		v, p, _ := strings.Cut(sisa, "/")
		vendor = strings.TrimSpace(dekodeURI(v))
		produk = strings.TrimSpace(dekodeURI(p))
	}
	// Printer jaringan tidak membawa vendor di URI-nya; lpinfo -l menaruhnya di
	// baris info, mis. "Canon iP2700 series".
	if vendor == "" && info != "" {
		v, p, ok := strings.Cut(strings.TrimSpace(info), " ")
		vendor = v
		if ok {
			produk = p
		}
	}
	if produk == "" {
		produk = info
	}
	return vendor, produk
}

// dekodeURI membuka escape persen ala %20 tanpa menyeret net/url untuk satu
// pemakaian; nilai yang tidak valid dikembalikan apa adanya.
func dekodeURI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 2
				continue
			}
		}
		if s[i] == '+' {
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// printerDeteksi menggabungkan perangkat yang terpasang, driver yang tersedia,
// dan antrean yang sudah ada — tiga hal yang harus dilihat bersamaan supaya UI
// bisa memberi satu tindakan yang tepat per printer.
func printerDeteksi() ([]helperproto.PrinterDeteksi, error) {
	if !cupsAda() {
		return []helperproto.PrinterDeteksi{}, nil
	}
	devices, _ := printerDevices()
	models, _ := printerModels()
	terdaftar := map[string]bool{}
	if list, err := printerList(); err == nil {
		for _, p := range list {
			if p.URI != "" {
				terdaftar[p.URI] = true
			}
		}
	}

	out := []helperproto.PrinterDeteksi{}
	for _, d := range devices {
		// Entri "network socket" dan sejenisnya adalah backend, bukan printer.
		if !strings.Contains(d.URI, "://") || strings.HasSuffix(d.URI, "://") {
			continue
		}
		vendor, produk := vendorProdukDariURI(d.URI, d.Info)
		det := helperproto.PrinterDeteksi{
			URI: d.URI, Info: d.Info, Vendor: vendor, Produk: produk,
			SudahTerdaftar: terdaftar[d.URI],
		}
		if m, nama, ok := cariModelCocok(models, produk); ok {
			det.Model, det.ModelName, det.SiapPakai = m, nama, true
		} else {
			det.PaketDriver = paketDriverUntuk(vendor)
		}
		out = append(out, det)
	}
	return out, nil
}

// cariModelCocok mencari PPD yang namanya memuat kunci model printer.
func cariModelCocok(models []helperproto.PrinterModel, produk string) (string, string, bool) {
	kunci := kunciCocok(produk)
	// Kunci terlalu pendek akan cocok dengan apa saja — "s" atau "10" bisa
	// mengenai ratusan PPD yang tidak ada hubungannya dengan printer ini.
	if len(kunci) < 4 {
		return "", "", false
	}
	for _, m := range models {
		if strings.Contains(normalisasiModel(m.Name), kunci) {
			return m.Model, m.Name, true
		}
	}
	return "", "", false
}

func paketDriverUntuk(vendor string) []string {
	if p, ok := paketDriverPrinter[strings.ToLower(strings.TrimSpace(vendor))]; ok {
		return p
	}
	return paketDriverPrinter["generic"]
}

// printerDriverInstall memasang paket driver untuk satu vendor. Vendor yang
// tidak ada di whitelist ditolak, bukan dialihkan diam-diam ke paket generik:
// memasang paket yang tidak diminta atas nama user bukan hal yang boleh
// terjadi tanpa ia tahu apa yang dipasang.
func printerDriverInstall(vendor string) error {
	v := strings.ToLower(strings.TrimSpace(vendor))
	paket, ok := paketDriverPrinter[v]
	if !ok {
		return errInvalid("vendor printer %q tidak dikenal", vendor)
	}
	if err := aptInstall(paket...); err != nil {
		return err
	}
	// PPD baru hanya terlihat setelah cupsd memuat ulang katalog drivernya.
	_, _ = run("systemctl", "restart", "cups")
	return nil
}
