export type Workspace = {
  id: string
  name: string
  slug: string
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

export type CreateWorkspaceInput = {
  name: string
  slug: string
}
