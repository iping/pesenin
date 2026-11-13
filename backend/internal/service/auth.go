package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pesenin.product.biz.id/api/internal/db"
)

// EnsureSellerAccount creates the account, wallet, and signup koin once.
// Platform admins who are not sellers stay without an account until they ask.
func (a *App) EnsureSellerAccount(ctx context.Context, user db.User) (db.Account, db.Wallet, bool, error) {
	acc, err := a.Q.GetAccountByOwner(ctx, user.ID)
	if err == nil {
		w, err := a.Q.GetWalletByAccount(ctx, acc.ID)
		return acc, w, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Account{}, db.Wallet{}, false, err
	}
	settings, err := a.LoadSettings(ctx)
	if err != nil {
		return db.Account{}, db.Wallet{}, false, err
	}
	var account db.Account
	var wallet db.Wallet
	err = a.tx(ctx, func(q *db.Queries) error {
		var err error
		name := user.Name
		if name == "" {
			name = user.Email
		}
		account, err = q.CreateAccount(ctx, db.CreateAccountParams{OwnerUserID: user.ID, Name: name})
		if err != nil {
			return err
		}
		if err := q.InsertOwnerMember(ctx, db.InsertOwnerMemberParams{AccountID: account.ID, UserID: user.ID}); err != nil {
			return err
		}
		wallet, err = q.CreateWallet(ctx, account.ID)
		if err != nil {
			return err
		}
		if settings.SignupFree != 0 {
			if _, _, err := applyLedger(ctx, q, wallet.ID, ledgerIn{
				Type: "promo_free_credit", Amount: settings.SignupFree,
				Key: "promo_free_credit:" + account.ID.String(), Reason: "Koin gratis pendaftaran",
			}); err != nil {
				return err
			}
			wallet.BalanceKoin = settings.SignupFree
		}
		a.audit(ctx, q, &account.ID, &user.ID, "account.create", "account", account.ID.String(), nil, map[string]any{"free": settings.SignupFree})
		return nil
	})
	return account, wallet, true, err
}

func (a *App) DevLogin(ctx context.Context, email, name string) (db.User, error) {
	if !a.Cfg.DevLogin {
		return db.User{}, E(404, "disabled", "Login demo dimatikan.")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(email) > 200 {
		return db.User{}, E(400, "bad_email", "Email belum pas.")
	}
	user, err := a.Q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		admin := email == a.Cfg.AdminEmail
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		user, err = a.Q.CreateUser(ctx, db.CreateUserParams{Email: email, Name: name, IsPlatformAdmin: admin})
	}
	if err != nil {
		return db.User{}, err
	}
	if !user.IsPlatformAdmin {
		if _, _, _, err := a.EnsureSellerAccount(ctx, user); err != nil {
			return db.User{}, err
		}
	}
	return user, nil
}

func (a *App) UpsertGoogle(ctx context.Context, sub, email, name, avatar string) (db.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var avatarPtr *string
	if avatar != "" {
		avatarPtr = &avatar
	}
	subCopy := sub
	if u, err := a.Q.GetUserByGoogleSub(ctx, &subCopy); err == nil {
		return u, nil
	}
	if u, err := a.Q.GetUserByEmail(ctx, email); err == nil {
		if u.GoogleSub == nil {
			if _, err := a.Pool.Exec(ctx, `UPDATE users SET google_sub = $1, avatar_url = COALESCE($2, avatar_url), name = CASE WHEN $3 <> '' THEN $3 ELSE name END WHERE id = $4`, sub, avatarPtr, name, u.ID); err != nil {
				return db.User{}, err
			}
		}
		u, err = a.Q.GetUserByID(ctx, u.ID)
		if err != nil {
			return db.User{}, err
		}
		if !u.IsPlatformAdmin {
			_, _, _, err = a.EnsureSellerAccount(ctx, u)
		}
		return u, err
	}
	u, err := a.Q.UpsertGoogleUser(ctx, db.UpsertGoogleUserParams{Email: email, Name: name, AvatarUrl: avatarPtr, GoogleSub: &subCopy})
	if err != nil {
		return db.User{}, err
	}
	_ = a.Q.InsertIdentity(ctx, db.InsertIdentityParams{UserID: u.ID, Provider: "google", ProviderSubject: sub})
	if !u.IsPlatformAdmin {
		if _, _, _, err := a.EnsureSellerAccount(ctx, u); err != nil {
			return db.User{}, err
		}
	}
	return u, nil
}

func (a *App) StartSession(ctx context.Context, userID uuid.UUID, ua, ip string) (string, error) {
	raw, err := sessionToken()
	if err != nil {
		return "", err
	}
	_, err = a.Q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hashToken(raw), UserID: userID, ExpiresAt: time.Now().Add(a.Cfg.SessionTTL),
		UserAgent: strPtrOrNil(ua), Ip: strPtrOrNil(ip),
	})
	return raw, err
}

func (a *App) UserFromSession(ctx context.Context, raw string) (db.User, error) {
	if raw == "" {
		return db.User{}, E(401, "unauthenticated", "Masuk dulu ya.")
	}
	s, err := a.Q.GetSessionByHash(ctx, hashToken(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, E(401, "unauthenticated", "Sesi habis. Masuk lagi ya.")
	}
	if err != nil {
		return db.User{}, err
	}
	return a.Q.GetUserByID(ctx, s.UserID)
}

func (a *App) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	return a.Q.DeleteSessionByHash(ctx, hashToken(raw))
}

func (a *App) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	return a.Q.DeleteUserSessions(ctx, userID)
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Me bundles the seller home payload.
type Me struct {
	User    db.User
	Account *db.Account
	Wallet  *db.Wallet
	Brands  []db.Brand
	Stores  map[uuid.UUID][]db.Store
	Locked  bool
	Low     bool
	Floor   int64
	Fee     int64
}

func (a *App) LoadMe(ctx context.Context, user db.User) (Me, error) {
	me := Me{User: user, Stores: map[uuid.UUID][]db.Store{}}
	settings, err := a.LoadSettings(ctx)
	if err != nil {
		return me, err
	}
	me.Floor = settings.GraceFloor
	me.Fee = settings.OrderFee
	acc, err := a.Q.GetAccountByOwner(ctx, user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return me, nil
	}
	if err != nil {
		return me, err
	}
	me.Account = &acc
	w, err := a.Q.GetWalletByAccount(ctx, acc.ID)
	if err != nil {
		return me, err
	}
	me.Wallet = &w
	me.Locked = w.BalanceKoin <= settings.GraceFloor
	me.Low = w.BalanceKoin < settings.LowBalance
	brands, err := a.Q.ListBrandsByAccount(ctx, acc.ID)
	if err != nil {
		return me, err
	}
	me.Brands = brands
	for _, b := range brands {
		stores, err := a.Q.ListStores(ctx, b.ID)
		if err != nil {
			return me, err
		}
		me.Stores[b.ID] = stores
	}
	return me, nil
}
