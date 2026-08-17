package game

import (
	"errors"
	"math/rand"
)

// Bet types
const (
	BetEven = "even"
	BetOdd  = "odd"
)

// Results
const (
	ResultWin  = "win"
	ResultLose = "lose"
)

// Actions (WebSocket message routing)
const (
	ActionWallet  = "wallet"
	ActionPlay    = "play"
	ActionEndPlay = "endplay"
)

var (
	ErrInvalidBetAmount = errors.New("betAmount must be greater than 0")
	ErrInvalidBetType   = errors.New("type must be 'even' or 'odd'")
)

// ValidateBet() checks that betAmount is positive and betType is valid
func ValidateBet(betAmount float64, betType string) error {
	if betAmount <= 0 {
		return ErrInvalidBetAmount
	}
	if betType != BetEven && betType != BetOdd {
		return ErrInvalidBetType
	}
	return nil
}

// RollDice() returns a random int between 1 and 6
func RollDice() int {
	return rand.Intn(6) + 1
}

// Play() validates the bet, rolls the dice, and returns the drawn number,
// result ("win"/"lose"), and pending winnings
// pendingWin = betAmount*2 on win, 0 on loss
func Play(betAmount float64, betType string) (drawnNumber int, result string, pendingWin float64, err error) {
	if err = ValidateBet(betAmount, betType); err != nil {
		return
	}

	drawnNumber = RollDice()

	isEven := drawnNumber%2 == 0
	won := (isEven && betType == BetEven) || (!isEven && betType == BetOdd)

	if won {
		result = ResultWin
		pendingWin = betAmount * 2
	} else {
		result = ResultLose
		pendingWin = 0
	}

	return
}
