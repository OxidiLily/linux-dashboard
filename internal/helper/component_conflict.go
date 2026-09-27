package helper

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// portTerpakai adalah listener yang sedang menguasai satu port di mesin.
type portTerpakai struct {
	Port   string
	Proto  string
	Proses string
	PID    int
}

var prosesSSRe = regexp.MustCompile(`users:\(\("([^"\n]+)",pid=([0-9]+)`) // ss -ltnup

func bacaPortTerpakai() ([]portTerpakai, error) {
	res, err := run("ss", "-ltnupH")
	if err != nil {
		return nil, fmt.Errorf("tidak bisa memeriksa port aktif: %w", err)
	}
	return parsePortTerpakai(res.Stdout), nil
}

func parsePortTerpakai(keluaran string) []portTerpakai {
	var out []portTerpakai
	for _, baris := range strings.Split(keluaran, "\n") {
		f := strings.Fields(baris)
		if len(f) < 5 {
			continue
		}
		proto := strings.ToLower(f[0])
		if proto != "tcp" && proto != "udp" {
			continue
		}
		alamat := f[4]
		if proto == "udp" {
			// Pada keluaran ss UDP, alamat lokal tetap field ke-5. Cabang ini
			// hanya mendokumentasikan bahwa LISTEN/UNCONN tidak memengaruhi parser.
			alamat = f[4]
		}
		i := strings.LastIndex(alamat, ":")
		if i < 0 || i+1 >= len(alamat) {
			continue
		}
		port := strings.Trim(alamat[i+1:], "[]")
		if !portRe.MatchString(port) {
			continue
		}
		item := portTerpakai{Port: port, Proto: proto}
		if m := prosesSSRe.FindStringSubmatch(baris); len(m) == 3 {
			item.Proses = m[1]
			item.PID, _ = strconv.Atoi(m[2])
		}
		out = append(out, item)
	}
	return out
}

func cekKonflikPort(c *component, terpakai []portTerpakai) error {
	for _, perlu := range c.ports {
		// Rentang UFW bukan listener tunggal. Komponen yang butuh preflight
		// mendeklarasikan port tunggal (nginx 80/443); rentang tetap dibiarkan
		// untuk firewall seperti sebelumnya.
		if strings.Contains(perlu.Port, ":") {
			continue
		}
		for _, aktif := range terpakai {
			if perlu.Port != aktif.Port || perlu.Proto != aktif.Proto {
				continue
			}
			pemilik := aktif.Proses
			if pemilik == "" {
				pemilik = "proses lain"
			}
			if aktif.PID > 0 {
				pemilik += fmt.Sprintf(" (PID %d)", aktif.PID)
			}
			return &helperErr{
				code:   helperproto.ErrPortKonflik,
				kodeUI: helperproto.ErrPortKonflik,
				params: []string{c.Name, perlu.Port + "/" + perlu.Proto, pemilik},
				msg: fmt.Sprintf("komponen %s tidak bisa dipasang: port %s/%s sudah dipakai %s",
					c.Name, perlu.Port, perlu.Proto, pemilik),
			}
		}
	}
	return nil
}

func preflightKonflikPort(c *component) error {
	if len(c.ports) == 0 {
		return nil
	}
	terpakai, err := bacaPortTerpakai()
	if err != nil {
		return err
	}
	return cekKonflikPort(c, terpakai)
}
