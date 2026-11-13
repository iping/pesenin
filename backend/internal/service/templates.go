package service

const defaultConfirm = `Halo {nama}, pesanan {kode} di {brand} sudah kami terima.
Rincian:
{rincian}
Total transfer: Rp {total} (mohon transfer PERSIS sampai 3 digit terakhir)
{bank} {norek} a.n. {pemilik}
Upload bukti di: {link_status}
Batas bayar: {batas_bayar}`

const defaultRemind = `Halo {nama}, pesanan {kode} belum kami terima pembayarannya. Total Rp {total}. Bayar sebelum {batas_bayar} ya: {link_status}`

const defaultReady = `Halo {nama}, pesanan {kode} sudah siap! Ambil di {store}, {alamat}. Terima kasih.`
