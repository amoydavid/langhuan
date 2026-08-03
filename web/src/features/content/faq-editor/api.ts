import { apiClient } from '@/lib/api/client'
import type { CreateFAQInput, UpdateFAQInput } from './schemas'
import { faqDocumentSchema } from './schemas'

function workspacePath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}`
}

function faqDocumentPath(workspaceSlug: string, documentId: string) {
  return `${workspacePath(workspaceSlug)}/documents/${encodeURIComponent(documentId)}/faq`
}

export async function createFAQDocument(
  workspaceSlug: string,
  kbId: string,
  input: CreateFAQInput
) {
  const response = await apiClient.post<unknown>(
    `${workspacePath(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/documents/faq`,
    input
  )
  return faqDocumentSchema.parse(response.data)
}

export async function getFAQDocument(
  workspaceSlug: string,
  documentId: string
) {
  const response = await apiClient.get<unknown>(
    faqDocumentPath(workspaceSlug, documentId)
  )
  return faqDocumentSchema.parse(response.data)
}

export async function updateFAQDocument(
  workspaceSlug: string,
  documentId: string,
  input: UpdateFAQInput
) {
  const response = await apiClient.put<unknown>(
    faqDocumentPath(workspaceSlug, documentId),
    input
  )
  return faqDocumentSchema.parse(response.data)
}
