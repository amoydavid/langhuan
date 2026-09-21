import { describe, expect, it } from 'vitest'
import { withoutWebDocuments } from './file-tree-model'
import type { FileTreeNode } from './schemas'

function node(
  id: string,
  nodeType: FileTreeNode['node_type'],
  documentId: string | null,
  children: FileTreeNode[] = []
): FileTreeNode {
  return {
    id,
    parent_id: null,
    node_type: nodeType,
    name: id,
    document_id: documentId,
    path: `/${id}`,
    children,
  }
}

describe('withoutWebDocuments', () => {
  const webDoc = 'web-doc-1'
  const fileDoc = 'file-doc-1'

  it('剪掉 web 文档节点，保留上传文件与普通文件夹', () => {
    const root = node('root', 'root', null, [
      node('upload', 'file', fileDoc),
      node('folder-manual', 'folder', null),
    ])
    const result = withoutWebDocuments(root, new Set([webDoc]))
    expect(result.children.map((c) => c.id)).toEqual([
      'upload',
      'folder-manual',
    ])
  })

  it('目录索引页（file 节点）连同其 web 子页面子树一起剪掉', () => {
    // /blog/ 是索引页（file），子页面挂在其下
    const root = node('root', 'root', null, [
      node('upload', 'file', fileDoc),
      node('blog-index', 'file', webDoc, [
        node('post-1', 'file', webDoc),
        node('post-2', 'file', webDoc),
      ]),
      node('folder-manual', 'folder', null),
    ])
    const result = withoutWebDocuments(root, new Set([webDoc]))
    expect(result.children.map((c) => c.id)).toEqual([
      'upload',
      'folder-manual',
    ])
  })

  it('混合文件夹：仅剪 web 子节点，保留非 web 子节点与文件夹本身', () => {
    const root = node('root', 'root', null, [
      node('mixed', 'folder', null, [
        node('upload-in-folder', 'file', fileDoc),
        node('web-in-folder', 'file', webDoc),
      ]),
    ])
    const result = withoutWebDocuments(root, new Set([webDoc]))
    const mixed = result.children[0]
    expect(mixed.id).toBe('mixed')
    expect(mixed.children.map((c) => c.id)).toEqual(['upload-in-folder'])
  })

  it('全 web 子树的文件夹保留为空文件夹（与新建空文件夹展示一致）', () => {
    const root = node('root', 'root', null, [
      node('blog-folder', 'folder', null, [node('post', 'file', webDoc)]),
    ])
    const result = withoutWebDocuments(root, new Set([webDoc]))
    const folder = result.children[0]
    expect(folder.id).toBe('blog-folder')
    expect(folder.children).toEqual([])
  })
})
