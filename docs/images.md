# Images

Photos stay small enough for mid-range Android phones on Indonesian mobile data. Transfer proofs stay sharp enough that the amount, account number, reference, and timestamp are readable when the seller zooms in.

The browser compresses before upload. The API always decodes and re-encodes. Client compression is not trusted.

## Budgets

The raw upload cap is **8 MB** (`images.MaxUploadBytes`). The handler and the service both enforce it. Stored files are the variants below, not the original camera file.

### Transfer proofs

Customer order proofs and koin top-up proofs use the same ladder. Nothing is upscaled.

| Variant | Max long edge | WebP quality | Use |
| --- | --- | --- | --- |
| full | 2400 | 86 | The file the seller opens. Small text has to survive zoom. |
| display | 1440 | 82 | Inline preview when a list shows more than one proof. |
| thumb | 480 | 74 | Tight list cells. |

The phone, before upload (`frontend/src/lib/images.ts`, profile `proof`): long edge 2400, WebP quality 0.90, and it will not go below 0.82 while trying to land under 850 KB. If the browser cannot encode WebP, the file is JPEG at a slightly higher quality.

Quality is not lowered further to chase a byte cap. A screenshot of a banking app is usually well under 500 KB after this. A detailed photo of a paper receipt can be larger, and that is preferable to smearing the digits.

### Product photos

| Variant | Max long edge | WebP quality | Use |
| --- | --- | --- | --- |
| display | 1600 | 80 | Retina phone width (about 430 CSS px at 3x is covered). |
| thumb | 480 | 74 | Store grid and seller lists (32–64 CSS px). |

The phone, before upload (profile `product`): long edge 1600, WebP quality 0.82, floor 0.74, aim under 380 KB.

## What the server stores

Package `backend/internal/images`. Decode JPEG, PNG, GIF (first frame), and WebP. JPEG EXIF orientation is applied, then the pixels are re-encoded, so GPS and other metadata are not copied. Proofs are flattened onto white. Resize uses Catmull-Rom. Encode is lossy WebP through libwebp compiled to WebAssembly, so the API stays `CGO_ENABLED=0` and the same bytes go to `STORAGE_DRIVER=local` and to S3/R2.

```
products/{id}/d1600-t480.webp    display bytes (this key is saved on the product)
products/{id}/t480.webp          thumb
proofs/{id}/f1800-d1080-t360.webp
proofs/{id}/d1080.webp
proofs/{id}/t360.webp
```

The numbers are the real pixel widths, so they are smaller when the source was already small. The UI builds `srcset` from the name. An older key such as `proofs/{uuid}.jpg` is still served as a single image.

Rejected: empty files, anything over 8 MB, SVG/HTML, more than 40 megapixels or a side over 12,000 px, and HEIC/AVIF. HEIC is converted in the browser (native decode when the phone can, otherwise a lazy `heic2any` import). The server does not link libheif.

## Delivery

- Product grids use `srcset` and `sizes`. A 64 px cell on a 3x screen selects the 480 px thumb.
- A proof on the order or admin page loads the **full** file, with `object-contain` so the whole slip is visible. Tap opens it at about one device pixel per source pixel (`fullWidth / devicePixelRatio`), which you can scroll. That is what keeps account numbers readable when browser pinch-zoom is awkward in the installed PWA.
- Images lazy-load. While they load, and when the photo is missing, the grey Pingping placeholders in `frontend/static/brand/` stay on screen.
- Product URLs: `Cache-Control: public, max-age=31536000, immutable, no-transform`.
- Proof and top-up URLs: `private, max-age=31536000, immutable, no-transform`. A shared cache must not keep photos of bank transfers. The UUID path does not change, so the browser can keep the file.
- The same cache header is stored on the R2 object. Local disk has no object metadata; the API sets the header when it serves `/api/v1/media/...`.
- Static brand files keep stable paths (`/icon-512.png`, `/brand/loading.gif`), so they are not marked immutable.

## Measured samples

From `go test ./internal/images/ -v`. Gradient frames are a floor (they compress unusually well). The noise row is a ceiling, not a photo. `logo.jpg` is a real file in the repo.

| Sample | Before | After |
| --- | --- | --- |
| White screenshot, 1080×1920, digits ~20 px tall | JPEG 35.0 KB | full WebP 4.0 KB (1080×1920, q86). Stroke stays dark, background stays white. |
| Smooth photo, 2000×1500, JPEG q90 | 66.2 KB | display 7.6 KB (1600×1200, q80), thumb 1.1 KB |
| Camera-shaped frame, 3024×4032, JPEG q92 | 238.6 KB | full 16.2 KB (1800×2400, q86), display 6.5 KB (1080×1440), thumb 1.0 KB |
| `brand/logo.jpg`, 1024×1024 | 177.1 KB | display 37.0 KB (1024×1024, q80), thumb 12.6 KB |
| Flat PNG, 800×800 | 18.5 KB | display WebP 3.0 KB, thumb 1.2 KB |
| High-frequency noise, 1600×1200, JPEG q90 | 1.58 MB | display WebP 1.19 MB (q80). Quality is not reduced to chase this. |

## Static brand files

PNG icons and placeholders were quantized with pngquant at quality 90–100 (near-lossless; skipped when the file did not shrink). `logo.jpg` was recompressed at a high JPEG setting. Animated GIFs gained an animated WebP sibling. The GIF stays as the fallback. Recompressing `logo.gif` itself made it larger, so the original GIF remains.

| File | Before | After |
| --- | --- | --- |
| `icon-1024.png` | 784 KB | 301 KB |
| `icon-512.png` | 283 KB | 105 KB |
| `icon-384.png` | 174 KB | 63 KB |
| `icon-maskable-512.png` | 191 KB | 84 KB |
| `icon-256.png` | 87 KB | 32 KB |
| `icon-192.png` | 53 KB | 19 KB |
| `apple-touch-icon.png` | 41 KB | 18 KB |
| `brand/icon.png` | 268 KB | 101 KB |
| `brand/placeholder-avatar.png` | 94 KB | 51 KB |
| `brand/placeholder-1x1.png` | 63 KB | 36 KB |
| `brand/placeholder-16x9.png` | 50 KB | 28 KB |
| `brand/logo.jpg` | 177 KB | 87 KB |
| `brand/loading.gif` | 532 KB | 532 KB (fallback) |
| `brand/loading.webp` | — | 287 KB (splash; browsers pick this) |
| `brand/logo.gif` | 2.41 MB | 2.41 MB (fallback, not used by a page today) |
| `brand/logo.webp` | — | 814 KB |
| Smaller icons and favicons | | about 25–65% smaller, except 16 px files that were already tiny |

PWA manifest icons stay PNG. The splash uses `<picture>`: animated WebP, then the GIF.
