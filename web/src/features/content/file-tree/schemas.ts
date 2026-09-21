import { z } from 'zod'
import i18n from '@/lib/i18n'

export type FileTreeNode = {
  id: string
  parent_id: string | null
  node_type: 'root' | 'folder' | 'file'
  name: string
  document_id: string | null
  path: string
  children: FileTreeNode[]
}

export const fileTreeNodeSchema: z.ZodType<FileTreeNode> = z.lazy(() =>
  z
    .object({
      id: z.uuid(),
      parent_id: z.uuid().nullable(),
      node_type: z.enum(['root', 'folder', 'file']),
      name: z.string().min(1),
      document_id: z.uuid().nullable(),
      path: z.string(),
      children: z.array(fileTreeNodeSchema),
    })
    .superRefine((node, context) => {
      if (node.node_type === 'file' && !node.document_id) {
        context.addIssue({
          code: 'custom',
          message: i18n.t('content.fileTree.schema.fileRequiresDocument'),
          path: ['document_id'],
        })
      }
      if (node.node_type !== 'file' && node.document_id) {
        context.addIssue({
          code: 'custom',
          message: i18n.t('content.fileTree.schema.folderCannotHaveDocument'),
          path: ['document_id'],
        })
      }
      // file 节点允许带 children：web 源的「目录索引页」即目录的代表文档，
      // 其子页面（URL path 前缀匹配）挂在索引页节点下（v1.3.0 web 源语义）。
    })
)

export const fileTreeSchema = z.object({
  workspace_id: z.uuid(),
  knowledge_base_id: z.uuid(),
  root: fileTreeNodeSchema,
})

export type FileTreeData = z.infer<typeof fileTreeSchema>
