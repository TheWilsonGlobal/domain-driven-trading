package seed_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/seed"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

type memRepo struct {
	accs   map[string]*account.Account
	saves  int
	getErr error
}

func (r *memRepo) GetByID(_ context.Context, id string) (*account.Account, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if a, ok := r.accs[id]; ok {
		return a, nil
	}
	return nil, common.ErrAccountNotFound
}

func (r *memRepo) Save(_ context.Context, a *account.Account) error {
	r.saves++
	r.accs[a.ID] = a
	return nil
}

func TestAccounts_SeedsMissingOnlyAndNeverOverwrites(t *testing.T) {
	existing := account.NewAccount("ACC_APX_001", 42)
	repo := &memRepo{accs: map[string]*account.Account{"ACC_APX_001": existing}}

	created, err := seed.Accounts(context.Background(), repo, seed.Defaults(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if repo.accs["ACC_APX_001"] != existing || existing.CashBalance != 42 {
		t.Fatalf("existing account overwritten: %+v", repo.accs["ACC_APX_001"])
	}
	for _, id := range []string{"ACC_APX_002", "ACC_INST_MM", account.FeeCollectorID} {
		if _, ok := repo.accs[id]; !ok {
			t.Errorf("%s not seeded", id)
		}
	}
	if created != len(seed.Defaults())-1 || repo.saves != created {
		t.Errorf("created=%d saves=%d, want %d", created, repo.saves, len(seed.Defaults())-1)
	}
	// Second run is a no-op.
	repo.saves = 0
	created, err = seed.Accounts(context.Background(), repo, seed.Defaults(), slog.Default())
	if err != nil || created != 0 || repo.saves != 0 {
		t.Errorf("second run: created=%d saves=%d err=%v", created, repo.saves, err)
	}
}

func TestAccounts_EnsuresFeeCollectorEvenIfNotListed(t *testing.T) {
	repo := &memRepo{accs: map[string]*account.Account{}}
	if _, err := seed.Accounts(context.Background(), repo, []seed.Account{{ID: "ONLY_ONE", Cash: 5}}, nil); err != nil {
		t.Fatal(err)
	}
	if fc, ok := repo.accs[account.FeeCollectorID]; !ok || fc.CashBalance != 0 {
		t.Errorf("fee collector missing or funded: %+v", fc)
	}
}

func TestAccounts_RepoErrorAbortsAndSavesNothing(t *testing.T) {
	boom := errors.New("db down")
	repo := &memRepo{accs: map[string]*account.Account{}, getErr: boom}
	_, err := seed.Accounts(context.Background(), repo, seed.Defaults(), nil)
	if !errors.Is(err, boom) || repo.saves != 0 {
		t.Fatalf("err=%v saves=%d; a non-NotFound error must abort without saving", err, repo.saves)
	}
}

func TestParse(t *testing.T) {
	got, err := seed.Parse("A_1:1000:APX=10,HPG=5;B_2:0")
	if err != nil || len(got) != 2 || got[0].ID != "A_1" || got[0].Cash != 1000 ||
		got[0].Holdings["APX"] != 10 || got[0].Holdings["HPG"] != 5 || got[1].ID != "B_2" {
		t.Fatalf("got %+v err=%v", got, err)
	}
	for _, bad := range []string{"A", "A:x", "A:-1", "bad id:1", "A:1:APX", "A:1:APX=-1", "A:1:APX=x", "A:1;A:2"} {
		if _, err := seed.Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

func TestFromEnv(t *testing.T) {
	d, err := seed.FromEnv(func(string) string { return "" })
	if err != nil || len(d) != len(seed.Defaults()) {
		t.Fatalf("defaults: %v %v", d, err)
	}
	c, err := seed.FromEnv(func(k string) string {
		if k == "SEED_ACCOUNTS" {
			return "X:1"
		}
		return ""
	})
	if err != nil || len(c) != 1 || c[0].ID != "X" {
		t.Fatalf("custom: %v %v", c, err)
	}
}
