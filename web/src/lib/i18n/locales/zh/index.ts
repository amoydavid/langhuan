import { apiKeys } from './apiKeys'
import { auth } from './auth'
import { chunks } from './chunks'
import { common } from './common'
import { content } from './content'
import { contentFaq } from './contentFaq'
import { documents } from './documents'
import { errors } from './errors'
import { indexGenerations } from './indexGenerations'
import { integrations } from './integrations'
import { invitations } from './invitations'
import { jobs } from './jobs'
import { knowledgeBases } from './knowledgeBases'
import { members } from './members'
import { models } from './models'
import { retrieval } from './retrieval'
import { routes } from './routes'
import { searchSettings } from './searchSettings'
import { settings } from './settings'
import { ui } from './ui'
import { workspaceReadiness } from './workspaceReadiness'
import { workspaces } from './workspaces'

/**
 * 中文（默认）资源。作为 key 结构的权威来源：
 * 英文资源必须与本对象结构一致（见 locales/en/index.ts）。
 */
export const zh = {
  apiKeys,
  auth,
  chunks,
  common,
  content,
  contentFaq,
  documents,
  errors,
  indexGenerations,
  integrations,
  invitations,
  jobs,
  knowledgeBases,
  members,
  models,
  retrieval,
  searchSettings,
  routes,
  settings,
  workspaceReadiness,
  workspaces,
  ui,
}
