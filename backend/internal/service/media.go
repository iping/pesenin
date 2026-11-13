package service

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/google/uuid"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/images"
)

func prepareImage(r io.Reader, declared string, kind images.Kind) (images.Result, error) {
	if !images.AllowedDeclared(declared) {
		return images.Result{}, E(400, "bad_file", images.ErrType.Message)
	}
	data, err := images.ReadLimited(r, images.MaxUploadBytes)
	if err != nil {
		return images.Result{}, fileErr(err)
	}
	res, err := images.Process(data, kind)
	if err != nil {
		return images.Result{}, fileErr(err)
	}
	return res, nil
}

func fileErr(err error) error {
	var fe *images.FileError
	if errors.As(err, &fe) {
		return E(fe.Status, fe.Code, fe.Message)
	}
	return E(400, "bad_file", images.ErrDecode.Message)
}

func (a *App) putImage(ctx context.Context, dir string, res images.Result) (key string, size int, err error) {
	blobs := res.Blobs(dir)
	for _, b := range blobs {
		if err := a.Blob.Put(ctx, b.Key, bytes.NewReader(b.Data), b.MIME); err != nil {
			return "", 0, err
		}
		if b.Primary {
			key = b.Key
			size = len(b.Data)
		}
	}
	if key == "" {
		return "", 0, E(400, "bad_file", images.ErrDecode.Message)
	}
	return key, size, nil
}

// SaveProductPhoto validates, re-encodes, and stores display + thumb sizes.
func (a *App) SaveProductPhoto(ctx context.Context, userID, productID uuid.UUID, r io.Reader, declared string) (db.Product, error) {
	p, err := a.Q.GetProduct(ctx, productID)
	if err != nil {
		return db.Product{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, p.BrandID); err != nil {
		return db.Product{}, err
	}
	res, err := prepareImage(r, declared, images.Product)
	if err != nil {
		return db.Product{}, err
	}
	key, _, err := a.putImage(ctx, "products/"+uuid.NewString(), res)
	if err != nil {
		return db.Product{}, err
	}
	return a.SetProductPhoto(ctx, userID, productID, "/api/v1/media/"+key)
}
