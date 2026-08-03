package pipeline

import (
	"context"

	"github.com/dajee/langhuan/internal/domain/model"
)

type AssetStage struct{}

func NewAssetStage() AssetStage {
	return AssetStage{}
}

func (s AssetStage) Run(context.Context, *model.Document) error {
	return nil
}
