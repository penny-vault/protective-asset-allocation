package paa_test

import (
	"context"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/penny-vault/protective-asset-allocation/paa"
	"github.com/penny-vault/pvbt/asset"
	"github.com/penny-vault/pvbt/data"
	"github.com/penny-vault/pvbt/engine"
	"github.com/penny-vault/pvbt/portfolio"
)

var _ = Describe("ProtectiveAssetAllocation", func() {
	var (
		ctx       context.Context
		snap      *data.SnapshotProvider
		nyc       *time.Location
		startDate time.Time
		endDate   time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		nyc, err = time.LoadLocation("America/New_York")
		Expect(err).NotTo(HaveOccurred())

		snap, err = data.NewSnapshotProvider("testdata/snapshot.db")
		Expect(err).NotTo(HaveOccurred())

		startDate = time.Date(2024, 6, 1, 0, 0, 0, 0, nyc)
		endDate = time.Date(2026, 3, 1, 0, 0, 0, 0, nyc)
	})

	AfterEach(func() {
		if snap != nil {
			snap.Close()
		}
	})

	runBacktest := func() portfolio.Portfolio {
		strategy := &paa.ProtectiveAssetAllocation{}
		acct := portfolio.New(
			portfolio.WithCash(100000, startDate),
			portfolio.WithAllMetrics(),
		)

		eng := engine.New(strategy,
			engine.WithDataProvider(snap),
			engine.WithAssetProvider(snap),
			engine.WithAccount(acct),
		)

		result, err := eng.Backtest(ctx, startDate, endDate)
		Expect(err).NotTo(HaveOccurred())
		return result
	}

	It("produces expected returns and risk metrics", func() {
		result := runBacktest()

		summary, err := result.Summary()
		Expect(err).NotTo(HaveOccurred())
		Expect(summary.TWRR).To(BeNumerically("~", 0.3460, 0.01))
		Expect(summary.MaxDrawdown).To(BeNumerically(">", -0.10), "max drawdown should be better than -10%%")

		Expect(result.Value()).To(BeNumerically("~", 134603, 500))
	})

	It("trades all expected asset classes", func() {
		result := runBacktest()
		txns := result.Transactions()

		tickers := map[string]bool{}
		for _, t := range txns {
			if t.Type == asset.BuyTransaction || t.Type == asset.SellTransaction {
				tickers[t.Asset.Ticker] = true
			}
		}

		// Risk universe assets
		for _, ticker := range []string{"SPY", "QQQ", "IWM", "VGK", "EWJ", "EEM", "GLD", "GSG", "HYG", "IYR", "LQD"} {
			Expect(tickers).To(HaveKey(ticker))
		}

		// Protective universe assets
		for _, ticker := range []string{"IEF", "STIP"} {
			Expect(tickers).To(HaveKey(ticker))
		}
	})

	It("produces the expected trade sequence", func() {
		result := runBacktest()
		txns := result.Transactions()

		type trade struct {
			date   string
			txType asset.TransactionType
			ticker string
		}

		var trades []trade
		for _, t := range txns {
			if t.Type == asset.BuyTransaction || t.Type == asset.SellTransaction {
				trades = append(trades, trade{
					date:   t.Date.In(nyc).Format("2006-01-02"),
					txType: t.Type,
					ticker: t.Asset.Ticker,
				})
			}
		}

		sort.Slice(trades, func(i, j int) bool {
			if trades[i].date != trades[j].date {
				return trades[i].date < trades[j].date
			}
			if trades[i].txType != trades[j].txType {
				return trades[i].txType < trades[j].txType
			}
			return trades[i].ticker < trades[j].ticker
		})

		expected := []trade{
			{"2024-06-28", asset.BuyTransaction, "EEM"},
			{"2024-06-28", asset.BuyTransaction, "EWJ"},
			{"2024-06-28", asset.BuyTransaction, "GLD"},
			{"2024-06-28", asset.BuyTransaction, "QQQ"},
			{"2024-06-28", asset.BuyTransaction, "SPY"},
			{"2024-06-28", asset.BuyTransaction, "STIP"},
			{"2024-06-28", asset.BuyTransaction, "VGK"},
			{"2024-07-31", asset.BuyTransaction, "IEF"},
			{"2024-07-31", asset.BuyTransaction, "IWM"},
			{"2024-07-31", asset.BuyTransaction, "IYR"},
			{"2024-07-31", asset.BuyTransaction, "QQQ"},
			{"2024-07-31", asset.SellTransaction, "EEM"},
			{"2024-07-31", asset.SellTransaction, "EWJ"},
			{"2024-07-31", asset.SellTransaction, "GLD"},
			{"2024-07-31", asset.SellTransaction, "STIP"},
			{"2024-07-31", asset.SellTransaction, "VGK"},
			{"2024-08-30", asset.BuyTransaction, "IEF"},
			{"2024-08-30", asset.BuyTransaction, "IWM"},
			{"2024-08-30", asset.BuyTransaction, "VGK"},
			{"2024-08-30", asset.SellTransaction, "EWJ"},
			{"2024-08-30", asset.SellTransaction, "IYR"},
			{"2024-09-30", asset.BuyTransaction, "EEM"},
			{"2024-09-30", asset.BuyTransaction, "IEF"},
			{"2024-09-30", asset.BuyTransaction, "IWM"},
			{"2024-09-30", asset.SellTransaction, "GLD"},
			{"2024-09-30", asset.SellTransaction, "VGK"},
			{"2024-10-31", asset.BuyTransaction, "EEM"},
			{"2024-10-31", asset.BuyTransaction, "IYR"},
			{"2024-10-31", asset.BuyTransaction, "STIP"},
			{"2024-10-31", asset.SellTransaction, "GLD"},
			{"2024-10-31", asset.SellTransaction, "IEF"},
			{"2024-11-29", asset.BuyTransaction, "GLD"},
			{"2024-11-29", asset.BuyTransaction, "HYG"},
			{"2024-11-29", asset.BuyTransaction, "STIP"},
			{"2024-11-29", asset.SellTransaction, "EEM"},
			{"2024-11-29", asset.SellTransaction, "IWM"},
			{"2024-11-29", asset.SellTransaction, "IYR"},
			{"2024-12-31", asset.BuyTransaction, "STIP"},
			{"2024-12-31", asset.SellTransaction, "GLD"},
			{"2024-12-31", asset.SellTransaction, "HYG"},
			{"2024-12-31", asset.SellTransaction, "IWM"},
			{"2024-12-31", asset.SellTransaction, "IYR"},
			{"2024-12-31", asset.SellTransaction, "QQQ"},
			{"2024-12-31", asset.SellTransaction, "SPY"},
			{"2025-01-31", asset.BuyTransaction, "GLD"},
			{"2025-01-31", asset.BuyTransaction, "GSG"},
			{"2025-01-31", asset.BuyTransaction, "HYG"},
			{"2025-01-31", asset.BuyTransaction, "IWM"},
			{"2025-01-31", asset.BuyTransaction, "QQQ"},
			{"2025-01-31", asset.BuyTransaction, "SPY"},
			{"2025-01-31", asset.SellTransaction, "IYR"},
			{"2025-01-31", asset.SellTransaction, "STIP"},
			{"2025-02-28", asset.BuyTransaction, "IYR"},
			{"2025-02-28", asset.BuyTransaction, "VGK"},
			{"2025-02-28", asset.SellTransaction, "GLD"},
			{"2025-02-28", asset.SellTransaction, "GSG"},
			{"2025-02-28", asset.SellTransaction, "HYG"},
			{"2025-02-28", asset.SellTransaction, "IWM"},
			{"2025-02-28", asset.SellTransaction, "STIP"},
			{"2025-03-31", asset.BuyTransaction, "EEM"},
			{"2025-03-31", asset.BuyTransaction, "GSG"},
			{"2025-03-31", asset.BuyTransaction, "STIP"},
			{"2025-03-31", asset.SellTransaction, "GLD"},
			{"2025-03-31", asset.SellTransaction, "HYG"},
			{"2025-03-31", asset.SellTransaction, "IYR"},
			{"2025-03-31", asset.SellTransaction, "QQQ"},
			{"2025-03-31", asset.SellTransaction, "SPY"},
			{"2025-03-31", asset.SellTransaction, "VGK"},
			{"2025-04-30", asset.BuyTransaction, "EWJ"},
			{"2025-04-30", asset.BuyTransaction, "LQD"},
			{"2025-04-30", asset.BuyTransaction, "STIP"},
			{"2025-04-30", asset.SellTransaction, "EEM"},
			{"2025-04-30", asset.SellTransaction, "GLD"},
			{"2025-04-30", asset.SellTransaction, "GSG"},
			{"2025-04-30", asset.SellTransaction, "HYG"},
			{"2025-04-30", asset.SellTransaction, "IYR"},
			{"2025-04-30", asset.SellTransaction, "VGK"},
			{"2025-05-30", asset.BuyTransaction, "EEM"},
			{"2025-05-30", asset.BuyTransaction, "EWJ"},
			{"2025-05-30", asset.BuyTransaction, "GLD"},
			{"2025-05-30", asset.BuyTransaction, "QQQ"},
			{"2025-05-30", asset.BuyTransaction, "SPY"},
			{"2025-05-30", asset.BuyTransaction, "VGK"},
			{"2025-05-30", asset.SellTransaction, "HYG"},
			{"2025-05-30", asset.SellTransaction, "LQD"},
			{"2025-05-30", asset.SellTransaction, "STIP"},
			{"2025-06-30", asset.BuyTransaction, "EEM"},
			{"2025-06-30", asset.BuyTransaction, "EWJ"},
			{"2025-06-30", asset.BuyTransaction, "GLD"},
			{"2025-06-30", asset.BuyTransaction, "QQQ"},
			{"2025-06-30", asset.BuyTransaction, "SPY"},
			{"2025-06-30", asset.BuyTransaction, "VGK"},
			{"2025-06-30", asset.SellTransaction, "STIP"},
			{"2025-07-31", asset.BuyTransaction, "EWJ"},
			{"2025-07-31", asset.BuyTransaction, "VGK"},
			{"2025-07-31", asset.SellTransaction, "EEM"},
			{"2025-08-29", asset.BuyTransaction, "EEM"},
			{"2025-08-29", asset.BuyTransaction, "EWJ"},
			{"2025-08-29", asset.BuyTransaction, "GLD"},
			{"2025-08-29", asset.BuyTransaction, "QQQ"},
			{"2025-08-29", asset.BuyTransaction, "SPY"},
			{"2025-08-29", asset.BuyTransaction, "VGK"},
			{"2025-08-29", asset.SellTransaction, "STIP"},
			{"2025-09-30", asset.BuyTransaction, "EEM"},
			{"2025-09-30", asset.BuyTransaction, "EWJ"},
			{"2025-09-30", asset.BuyTransaction, "GLD"},
			{"2025-09-30", asset.BuyTransaction, "QQQ"},
			{"2025-09-30", asset.BuyTransaction, "SPY"},
			{"2025-09-30", asset.BuyTransaction, "VGK"},
			{"2025-09-30", asset.SellTransaction, "STIP"},
			{"2025-10-31", asset.BuyTransaction, "EEM"},
			{"2025-10-31", asset.BuyTransaction, "EWJ"},
			{"2025-10-31", asset.BuyTransaction, "IEF"},
			{"2025-10-31", asset.BuyTransaction, "VGK"},
			{"2025-10-31", asset.SellTransaction, "EEM"},
			{"2025-10-31", asset.SellTransaction, "EWJ"},
			{"2025-10-31", asset.SellTransaction, "GLD"},
			{"2025-10-31", asset.SellTransaction, "QQQ"},
			{"2025-10-31", asset.SellTransaction, "SPY"},
			{"2025-10-31", asset.SellTransaction, "VGK"},
			{"2025-11-28", asset.BuyTransaction, "EEM"},
			{"2025-11-28", asset.BuyTransaction, "EWJ"},
			{"2025-11-28", asset.BuyTransaction, "GLD"},
			{"2025-11-28", asset.BuyTransaction, "IWM"},
			{"2025-11-28", asset.BuyTransaction, "QQQ"},
			{"2025-11-28", asset.BuyTransaction, "VGK"},
			{"2025-11-28", asset.SellTransaction, "IEF"},
			{"2025-11-28", asset.SellTransaction, "SPY"},
			{"2025-12-31", asset.BuyTransaction, "EEM"},
			{"2025-12-31", asset.BuyTransaction, "EWJ"},
			{"2025-12-31", asset.BuyTransaction, "IWM"},
			{"2025-12-31", asset.SellTransaction, "VGK"},
			{"2026-01-30", asset.BuyTransaction, "EWJ"},
			{"2026-01-30", asset.BuyTransaction, "GSG"},
			{"2026-01-30", asset.BuyTransaction, "IWM"},
			{"2026-01-30", asset.BuyTransaction, "VGK"},
			{"2026-01-30", asset.SellTransaction, "EEM"},
			{"2026-01-30", asset.SellTransaction, "GLD"},
			{"2026-01-30", asset.SellTransaction, "QQQ"},
			{"2026-02-27", asset.BuyTransaction, "GSG"},
			{"2026-02-27", asset.BuyTransaction, "IWM"},
			{"2026-02-27", asset.BuyTransaction, "VGK"},
			{"2026-02-27", asset.SellTransaction, "EEM"},
			{"2026-02-27", asset.SellTransaction, "EWJ"},
			{"2026-02-27", asset.SellTransaction, "GLD"},
		}

		Expect(trades).To(HaveLen(len(expected)), "trade count mismatch")
		for i, exp := range expected {
			Expect(trades[i].date).To(Equal(exp.date), "trade %d date", i)
			Expect(trades[i].txType).To(Equal(exp.txType), "trade %d type", i)
			Expect(trades[i].ticker).To(Equal(exp.ticker), "trade %d ticker", i)
		}
	})
})
