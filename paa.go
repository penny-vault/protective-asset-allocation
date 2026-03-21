// Copyright 2021-2026
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/penny-vault/pvbt/asset"
	"github.com/penny-vault/pvbt/data"
	"github.com/penny-vault/pvbt/engine"
	"github.com/penny-vault/pvbt/portfolio"
	"github.com/penny-vault/pvbt/universe"
)

//go:embed README.md
var description string

type ProtectiveAssetAllocation struct {
	RiskUniverse       universe.Universe `pvbt:"risk-universe" desc:"List of ETF, Mutual Fund, or Stock tickers in the risk universe" default:"SPY,QQQ,IWM,VGK,EWJ,EEM,IYR,GSG,GLD,HYG,LQD,TLT" suggest:"PAA-Conservative=SPY,QQQ,IWM,VGK,EWJ,EEM,IYR,GSG,GLD,HYG,LQD,TLT|PAA0=SPY,QQQ,IWM,VGK,EWJ,EEM,IYR,GSG,GLD,HYG,LQD,TLT|PAA1=SPY,QQQ,IWM,VGK,EWJ,EEM,IYR,GSG,GLD,HYG,LQD,TLT|PAA2=SPY,QQQ,IWM,VGK,EWJ,EEM,IYR,GSG,GLD,HYG,LQD,TLT"`
	ProtectiveUniverse universe.Universe `pvbt:"protective-universe" desc:"Safe-haven bond assets for crash protection" default:"IEF,SHY,STIP" suggest:"PAA-Conservative=SHV|PAA0=IEF,SHY|PAA1=IEF,SHY|PAA2=IEF,SHY"`
	ProtectionFactor   int               `pvbt:"protection-factor" desc:"How protective the crash protection should be; higher is more protective" default:"2" suggest:"PAA-Conservative=2|PAA0=0|PAA1=1|PAA2=2"`
	Lookback           int               `pvbt:"lookback" desc:"Number of months for momentum lookback" default:"12" suggest:"PAA-Conservative=12|PAA0=12|PAA1=12|PAA2=12"`
	TopN               int               `pvbt:"top-n" desc:"Number of top risk assets to invest in" default:"6" suggest:"PAA-Conservative=6|PAA0=6|PAA1=6|PAA2=6"`
}

func (s *ProtectiveAssetAllocation) Name() string {
	return "Protective Asset Allocation"
}

func (s *ProtectiveAssetAllocation) Setup(_ *engine.Engine) {}

func (s *ProtectiveAssetAllocation) Describe() engine.StrategyDescription {
	return engine.StrategyDescription{
		ShortCode:   "paa",
		Description: description,
		Source:      "https://papers.ssrn.com/sol3/papers.cfm?abstract_id=2759734",
		Version:     "1.0.0",
		VersionDate: time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
		Schedule:    "@monthend",
		Benchmark:   "SHV",
	}
}

type assetScore struct {
	Asset asset.Asset
	Score float64
}

func (s *ProtectiveAssetAllocation) Compute(ctx context.Context, eng *engine.Engine, strategyPortfolio portfolio.Portfolio, batch *portfolio.Batch) error {
	// 1. Fetch lookback+1 month window of daily close prices for risk universe.
	riskDF, err := s.RiskUniverse.Window(ctx, portfolio.Months(s.Lookback+1), data.MetricClose)
	if err != nil {
		return fmt.Errorf("failed to fetch risk universe prices: %w", err)
	}

	// Fetch lookback+1 month window for protective universe.
	protDF, err := s.ProtectiveUniverse.Window(ctx, portfolio.Months(s.Lookback+1), data.MetricClose)
	if err != nil {
		return fmt.Errorf("failed to fetch protective universe prices: %w", err)
	}

	// 2. Downsample to monthly, drop NaN.
	riskPrices := riskDF.Downsample(data.Monthly).Last().Drop(math.NaN())
	protPrices := protDF.Downsample(data.Monthly).Last().Drop(math.NaN())

	// Need at least lookback+2 rows for SMA(lookback+1) to produce a value.
	if riskPrices.Len() < s.Lookback+2 || protPrices.Len() < s.Lookback+2 {
		return nil
	}

	// 3. Compute SMA-based momentum: MOM = (price / SMA(lookback+1) - 1) * 100
	smaWindow := s.Lookback + 1

	riskSMA := riskPrices.Rolling(smaWindow).Mean()
	riskMomentum := riskPrices.Div(riskSMA).AddScalar(-1).MulScalar(100).Drop(math.NaN())
	riskMomentum = riskMomentum.Last()

	protSMA := protPrices.Rolling(smaWindow).Mean()
	protMomentum := protPrices.Div(protSMA).AddScalar(-1).MulScalar(100).Drop(math.NaN())
	protMomentum = protMomentum.Last()

	// Annotate portfolio with all momentum scores.
	for _, a := range riskMomentum.AssetList() {
		for _, m := range riskMomentum.MetricList() {
			v := riskMomentum.Value(a, m)
			if !math.IsNaN(v) {
				batch.Annotate(a.Ticker+"/"+string(m), strconv.FormatFloat(v, 'f', -1, 64))
			}
		}
	}

	for _, a := range protMomentum.AssetList() {
		for _, m := range protMomentum.MetricList() {
			v := protMomentum.Value(a, m)
			if !math.IsNaN(v) {
				batch.Annotate(a.Ticker+"/"+string(m), strconv.FormatFloat(v, 'f', -1, 64))
			}
		}
	}

	if riskMomentum.Len() == 0 || protMomentum.Len() == 0 {
		return nil
	}

	// 4. Count "good" risk assets (those with momentum > 0).
	riskAssets := riskMomentum.AssetList()
	goodCount := 0

	var scored []assetScore

	for _, a := range riskAssets {
		mom := riskMomentum.Value(a, data.MetricClose)
		if mom > 0 {
			goodCount++

			scored = append(scored, assetScore{Asset: a, Score: mom})
		}
	}

	// 5. Compute bond fraction.
	totalAssets := float64(len(riskAssets))
	n1 := float64(s.ProtectionFactor) * totalAssets / 4.0
	bf := (totalAssets - float64(goodCount)) / (totalAssets - n1)

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
		Date:    eng.CurrentDate(),
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

	// Annotate decision values.
	batch.Annotate("good", fmt.Sprintf("%d", goodCount))
	batch.Annotate("BF", fmt.Sprintf("%.2f", bf))
	batch.Annotate("SF", fmt.Sprintf("%.2f", sf))

	alloc.Justification = fmt.Sprintf("good=%d/%d BF=%.2f", goodCount, len(riskAssets), bf)

	// 10. Rebalance.
	if err := batch.RebalanceTo(ctx, alloc); err != nil {
		return fmt.Errorf("rebalance failed: %w", err)
	}

	return nil
}
