package api

import "strconv"

// batasUploadBerkas: sisa kuota permintaan, tapi tidak pernah lebih dari batas
// satu berkas. Dipisah dari handler supaya kasus batasnya (pas vs lewat) bisa
// diuji tanpa menjalankan upload sungguhan.
func batasUploadBerkas(total int64) int64 {
	sisa := int64(uploadMaxTotal) - total
	if sisa < 0 {
		return 0
	}
	if sisa < int64(uploadMaxBerkas) {
		return sisa
	}
	return int64(uploadMaxBerkas)
}

// pesanBatasUpload menyebut batas yang BENAR-BENAR kena.
//
// Sebelumnya pesan selalu menyebut batas per permintaan walaupun yang dilanggar
// batas per berkas: satu berkas 20 GiB dijawab "melebihi batas 64 GiB per
// permintaan", dan batas per berkas tidak disebut di mana pun. Operator yang
// membaca log lalu mencari sebab yang salah.
func pesanBatasUpload(jatah int64) string {
	if jatah < int64(uploadMaxBerkas) {
		return "melebihi batas " + strconv.Itoa(uploadMaxTotal>>30) + " GiB per permintaan"
	}
	return "berkas melebihi batas " + strconv.Itoa(uploadMaxBerkas>>30) + " GiB"
}
