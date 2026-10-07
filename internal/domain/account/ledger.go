package account

import (
	"fmt"
	"time"

	"github.com/vortex-trading/vortex-trading-engine/internal/domain/common"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/order"
)

type EntryType string

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

type AssetType string

const (
	AssetCash  AssetType = "CASH_VND"
	AssetStock AssetType = "STOCK_SHARE"
)

// LedgerEntry represents a single leg in a double-entry journal entry
type LedgerEntry struct {
	ID          string    `json:"entry_id"`
	TxID        string    `json:"tx_id"`
	AccountID   string    `json:"account_id"`
	Type        EntryType `json:"entry_type"` // DEBIT or CREDIT
	AssetType   AssetType `json:"asset_type"` // CASH_VND or STOCK_SHARE
	Symbol      string    `json:"symbol,omitempty"`
	Amount      int64     `json:"amount"`     // Positive integer (VND amount or number of shares)
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
}

// Transaction represents an atomic double-entry journal voucher where Sum(Debit) == Sum(Credit) per asset
type Transaction struct {
	ID        string        `json:"tx_id"`
	TradeID   string        `json:"trade_id"`
	Entries   []LedgerEntry `json:"entries"`
	Timestamp time.Time     `json:"timestamp"`
}

// CreateTradeSettlementJournal creates balanced double-entry vouchers for an executed trade
func CreateTradeSettlementJournal(
	tradeID string,
	buyerID string,
	sellerID string,
	feeCollectorID string,
	symbol order.Symbol,
	price order.Price,
	quantity order.Quantity,
) (*Transaction, error) {
	now := time.Now()
	txID := fmt.Sprintf("TX-%s", tradeID)

	notionalCash := price.Int64() * int64(quantity.Uint32())
	buyerFee := (notionalCash * DefaultFeeRateBasisPoints) / 10000
	sellerFee := (notionalCash * DefaultFeeRateBasisPoints) / 10000
	shares := int64(quantity.Uint32())

	// 1. Stock asset transfer:
	// Seller credited (decreased), Buyer debited (increased)
	stockDebitBuyer := LedgerEntry{
		ID:          fmt.Sprintf("%s-ENT-1", txID),
		TxID:        txID,
		AccountID:   buyerID,
		Type:        Debit,
		AssetType:   AssetStock,
		Symbol:      string(symbol),
		Amount:      shares,
		Description: fmt.Sprintf("Acquired %d shares of %s", shares, symbol),
		Timestamp:   now,
	}
	stockCreditSeller := LedgerEntry{
		ID:          fmt.Sprintf("%s-ENT-2", txID),
		TxID:        txID,
		AccountID:   sellerID,
		Type:        Credit,
		AssetType:   AssetStock,
		Symbol:      string(symbol),
		Amount:      shares,
		Description: fmt.Sprintf("Disposed %d shares of %s", shares, symbol),
		Timestamp:   now,
	}

	// 2. Cash settlement:
	// Buyer credits cash (outflow), Seller debits cash (inflow)
	cashCreditBuyer := LedgerEntry{
		ID:          fmt.Sprintf("%s-ENT-3", txID),
		TxID:        txID,
		AccountID:   buyerID,
		Type:        Credit,
		AssetType:   AssetCash,
		Amount:      notionalCash + buyerFee,
		Description: fmt.Sprintf("Cash payment for %s plus brokerage fee", symbol),
		Timestamp:   now,
	}
	cashDebitSeller := LedgerEntry{
		ID:          fmt.Sprintf("%s-ENT-4", txID),
		TxID:        txID,
		AccountID:   sellerID,
		Type:        Debit,
		AssetType:   AssetCash,
		Amount:      notionalCash - sellerFee,
		Description: fmt.Sprintf("Net cash proceeds from selling %s", symbol),
		Timestamp:   now,
	}

	// 3. Brokerage fee clearing
	feeCreditRevenue := LedgerEntry{
		ID:          fmt.Sprintf("%s-ENT-5", txID),
		TxID:        txID,
		AccountID:   feeCollectorID,
		Type:        Debit,
		AssetType:   AssetCash,
		Amount:      buyerFee + sellerFee,
		Description: fmt.Sprintf("Clearing exchange brokerage fees for trade %s", tradeID),
		Timestamp:   now,
	}

	entries := []LedgerEntry{
		stockDebitBuyer,
		stockCreditSeller,
		cashCreditBuyer,
		cashDebitSeller,
		feeCreditRevenue,
	}

	tx := &Transaction{
		ID:        txID,
		TradeID:   tradeID,
		Entries:   entries,
		Timestamp: now,
	}

	// Double-entry validation check: Total Stock Debit == Total Stock Credit, Total Cash Debit == Total Cash Credit
	var totalStockDebit, totalStockCredit int64
	var totalCashDebit, totalCashCredit int64

	for _, e := range entries {
		if e.Amount <= 0 {
			return nil, common.ErrNegativeLedgerAmount
		}
		if e.AssetType == AssetStock {
			if e.Type == Debit {
				totalStockDebit += e.Amount
			} else {
				totalStockCredit += e.Amount
			}
		} else if e.AssetType == AssetCash {
			if e.Type == Debit {
				totalCashDebit += e.Amount
			} else {
				totalCashCredit += e.Amount
			}
		}
	}

	if totalStockDebit != totalStockCredit || totalCashDebit != totalCashCredit {
		return nil, common.ErrUnbalancedLedger
	}

	return tx, nil
}
