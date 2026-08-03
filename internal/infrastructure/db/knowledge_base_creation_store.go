package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
)

type knowledgeBaseCreateTx struct {
	db *gorm.DB
}

// WithinWorkspace binds KnowledgeBase creation to one tenant-local transaction.
func (r *KnowledgeBaseRepository) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, service.KnowledgeBaseCreateTx) error,
) error {
	runner := NewWorkspaceTxRunner(r.db)
	return runner.WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &knowledgeBaseCreateTx{db: tx})
	})
}

func (tx *knowledgeBaseCreateTx) CreateKnowledgeBaseRootAndGeneration(
	ctx context.Context,
	kb *model.KnowledgeBase,
	root *model.FileTreeNode,
	generation *model.IndexGeneration,
) error {
	return tx.createRootGeneration(ctx, kb, root, generation, nil)
}

// CreateKnowledgeBaseRootGenerationAndBinding 在创建知识库初始状态的同时，把新
// 知识库原子加入调用 API Key 的绑定集合。bindAPIKeyID 为 nil 时不写绑定。
func (tx *knowledgeBaseCreateTx) CreateKnowledgeBaseRootGenerationAndBinding(
	ctx context.Context,
	kb *model.KnowledgeBase,
	root *model.FileTreeNode,
	generation *model.IndexGeneration,
	bindAPIKeyID *uuid.UUID,
) error {
	return tx.createRootGeneration(ctx, kb, root, generation, bindAPIKeyID)
}

func (tx *knowledgeBaseCreateTx) createRootGeneration(
	ctx context.Context,
	kb *model.KnowledgeBase,
	root *model.FileTreeNode,
	generation *model.IndexGeneration,
	bindAPIKeyID *uuid.UUID,
) error {
	if kb.WorkspaceID != root.WorkspaceID || kb.WorkspaceID != generation.WorkspaceID ||
		kb.ID != root.KnowledgeBaseID || kb.ID != generation.KnowledgeBaseID {
		return fmt.Errorf("创建知识库初始状态失败: lineage 不一致")
	}
	if err := tx.db.WithContext(ctx).Create(knowledgeBaseV2ToRow(kb)).Error; err != nil {
		return translateDBError(err, "创建知识库失败")
	}
	if err := tx.db.WithContext(ctx).Create(fileTreeNodeToRow(root)).Error; err != nil {
		return translateDBError(err, "创建知识库文件树根节点失败")
	}
	if err := tx.db.WithContext(ctx).Create(indexGenerationToRow(generation)).Error; err != nil {
		return translateDBError(err, "创建知识库初始索引代次失败")
	}
	if bindAPIKeyID != nil {
		binding := &WorkspaceAPIKeyKnowledgeBaseRow{
			APITokenID:      *bindAPIKeyID,
			WorkspaceID:     kb.WorkspaceID,
			KnowledgeBaseID: kb.ID,
			CreatedAt:       time.Now().UTC(),
		}
		if err := tx.db.WithContext(ctx).Create(binding).Error; err != nil {
			return translateDBError(err, "把新知识库加入 API Key 绑定失败")
		}
	}
	return nil
}
