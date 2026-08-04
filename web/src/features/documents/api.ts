import { apiClient } from '@/lib/api/client'
import { documentListResponseSchema, documentResponseSchema } from './schemas'
import type {
  Document,
  DocumentAsset,
  DocumentIngestResult,
  Job,
  UploadDocumentInput,
} from './types'

function workspacePath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}`
}

function knowledgeBaseDocumentsPath(workspaceSlug: string, kbId: string) {
  return `${workspacePath(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/documents`
}

export async function listDocuments(workspaceSlug: string, kbId: string) {
  const response = await apiClient.get<Document[]>(
    knowledgeBaseDocumentsPath(workspaceSlug, kbId)
  )
  return documentListResponseSchema.parse(response.data)
}

export async function uploadDocument(
  workspaceSlug: string,
  kbId: string,
  input: UploadDocumentInput
) {
  const body = new FormData()
  body.append('file', input.file)
  body.append('title', input.title)
  body.append('source_type', input.source_type)
  if (input.parent_node_id) body.append('parent_node_id', input.parent_node_id)
  if (input.node_name) body.append('node_name', input.node_name)

  const response = await apiClient.post<DocumentIngestResult>(
    knowledgeBaseDocumentsPath(workspaceSlug, kbId),
    body,
    { params: { dedupe: input.dedupe } }
  )
  return response.data
}

export async function getDocument(workspaceSlug: string, documentId: string) {
  const response = await apiClient.get<Document>(
    `${workspacePath(workspaceSlug)}/documents/${encodeURIComponent(documentId)}`
  )
  return documentResponseSchema.parse(response.data)
}

export async function getDocumentAssets(
  workspaceSlug: string,
  documentId: string
) {
  const response = await apiClient.get<DocumentAsset[]>(
    `${workspacePath(workspaceSlug)}/documents/${encodeURIComponent(documentId)}/assets`
  )
  return response.data
}

export async function deleteDocument(
  workspaceSlug: string,
  documentId: string
) {
  await apiClient.delete(
    `${workspacePath(workspaceSlug)}/documents/${encodeURIComponent(documentId)}`
  )
}

export async function getJob(workspaceSlug: string, jobId: string) {
  const response = await apiClient.get<Job>(
    `${workspacePath(workspaceSlug)}/jobs/${encodeURIComponent(jobId)}`
  )
  return response.data
}
