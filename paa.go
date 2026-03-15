package main

import (
	"context"
	_ "embed"
	"math"
	"sort"
	"time"

	"github.com/penny-vault/pvbt/asset"
	"github.com/penny-vault/pvbt/data"
	"github.com/penny-vault/pvbt/engine"
	"github.com/penny-vault/pvbt/portfolio"
	"github.com/penny-vault/pvbt/tradecron"
	"github.com/penny-vault/pvbt/universe"
	"github.com/rs/zerolog"
)

//go:embed README.md
var description string

type ProtectiveAssetAllocation struct {
	RiskUniverse       universe.Universe `pvbt:"risk-universe" desc:"List of ETF, Mutual Fund, or Stock tickers in the risk universe" default:"SPY,QQQ,IWM,VGK,EWJ,EEM,IYR,GSG,GLD,HYG,LQD,TLT" suggest:"Default=SPY,QQQ,IWM,VGK,EWJ,EEM,IYR,GSG,GLD,HYG,LQD,TLT"`
	ProtectiveUniverse universe.Universe `pvbt:"protective-universe" desc:"Safe-haven bond assets for crash protection" default:"IEF,SHY,STIP" suggest:"Default=IEF,SHY,STIP|Conservative=SHV"`
	ProtectionFactor   int               `pvbt:"protection-factor" desc:"How protective the crash protection should be; higher is more protective" default:"2"`
	Lookback           int               `pvbt:"lookback" desc:"Number of months for momentum lookback" default:"12"`
	TopN               int               `pvbt:"top-n" desc:"Number of top risk assets to invest in" default:"6"`
}

func (s *ProtectiveAssetAllocation) Name() string {
	return "Protective Asset Allocation"
}

func (s *ProtectiveAssetAllocation) Setup(e *engine.Engine) {
	tc, err := tradecron.New("@monthend", tradecron.MarketHours{Open: 930, Close: 1600})
	if err != nil {
		panic(err)
	}
	e.Schedule(tc)
	e.SetBenchmark(e.Asset("SHV"))
	e.RiskFreeAsset(e.Asset("DGS3MO"))
}

func (s *ProtectiveAssetAllocation) Describe() engine.StrategyDescription {
	return engine.StrategyDescription{
		ShortCode:   "paa",
		Description: description,
		Source:      "https://papers.ssrn.com/sol3/papers.cfm?abstract_id=2759734",
		Version:     "1.0.0",
		VersionDate: time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
	}
}

type assetScore struct {
	Asset asset.Asset
	Score float64
}

func (s *ProtectiveAssetAllocation) Compute(ctx context.Context, e *engine.Engine, p portfolio.Portfolio) {
	log := zerolog.Ctx(ctx)

	// 1. Fetch lookback+1 month window of daily close prices for risk universe.
	riskDF, err := s.RiskUniverse.Window(ctx, portfolio.Months(s.Lookback+1), data.MetricClose)
	if err != nil {
		log.Error().Err(err).Msg("failed to fetch risk universe prices")
		return
	}

	// Fetch lookback+1 month window for protective universe.
	protDF, err := s.ProtectiveUniverse.Window(ctx, portfolio.Months(s.Lookback+1), data.MetricClose)
	if err != nil {
		log.Error().Err(err).Msg("failed to fetch protective universe prices")
		return
	}

	// 2. Downsample to monthly, drop NaN.
	riskPrices := riskDF.Downsample(data.Monthly).Last().Drop(math.NaN())
	protPrices := protDF.Downsample(data.Monthly).Last().Drop(math.NaN())

	// Need at least lookback+2 rows for SMA(lookback+1) to produce a value.
	if riskPrices.Len() < s.Lookback+2 || protPrices.Len() < s.Lookback+2 {
		return
	}

	// 3. Compute SMA-based momentum: MOM = (price / SMA(lookback+1) - 1) * 100
	smaWindow := s.Lookback + 1

	riskSMA := riskPrices.Rolling(smaWindow).Mean()
	riskMomentum := riskPrices.Div(riskSMA).AddScalar(-1).MulScalar(100).Drop(math.NaN())
	riskMomentum = riskMomentum.Last()

	protSMA := protPrices.Rolling(smaWindow).Mean()
	protMomentum := protPrices.Div(protSMA).AddScalar(-1).MulScalar(100).Drop(math.NaN())
	protMomentum = protMomentum.Last()

	if riskMomentum.Len() == 0 || protMomentum.Len() == 0 {
		return
	}

	// 4. Count "good" risk assets (those with momentum > 0).
	riskAssets := riskMomentum.AssetList()
	n := 0
	var scored []assetScore
	for _, a := range riskAssets {
		mom := riskMomentum.Value(a, data.MetricClose)
		if mom > 0 {
			n++
			scored = append(scored, assetScore{Asset: a, Score: mom})
		}
	}

	// 5. Compute bond fraction.
	N := float64(len(riskAssets))
	n1 := float64(s.ProtectionFactor) * N / 4.0
	bf := (N - float64(n)) / (N - n1)
	if bf > 1.0 {
		bf = 1.0
	}
	if bf < 0.0 {
		bf = 0.0
	}

	// 6. Stock fraction.
	sf := 1.0 - bf

	// 7. Select top-N risk assets with positive momentum, sorted by score descending.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > s.TopN {
		scored = scored[:s.TopN]
	}

	// 8. Select highest-momentum protective asset.
	protAssets := protMomentum.AssetList()
	var bestProtAsset asset.Asset
	bestProtScore := math.Inf(-1)
	for _, a := range protAssets {
		mom := protMomentum.Value(a, data.MetricClose)
		if mom > bestProtScore {
			bestProtScore = mom
			bestProtAsset = a
		}
	}

	// 9. Build allocation.
	alloc := portfolio.Allocation{
		Date:    e.CurrentDate(),
		Members: make(map[asset.Asset]float64),
	}

	if len(scored) > 0 && sf > 0 {
		weight := sf / float64(len(scored))
		for _, s := range scored {
			alloc.Members[s.Asset] = weight
		}
	}

	if bf > 0 && bestProtAsset != (asset.Asset{}) {
		alloc.Members[bestProtAsset] = bf
	}

	// 10. Rebalance.
	if err := p.RebalanceTo(ctx, alloc); err != nil {
		log.Error().Err(err).Msg("rebalance failed")
	}
}
