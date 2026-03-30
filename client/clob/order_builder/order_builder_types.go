package order_builder

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/lw396/polymarket-go/client/constants"
	"github.com/lw396/polymarket-go/client/signer"
)

type OrderBuilder struct {
	Signer  *signer.Signer
	SigType constants.SigType
	Funder  common.Address
}
