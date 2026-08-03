import type { zh } from '../zh'
import { apiKeys } from './apiKeys'
import { auth } from './auth'
import { chunks } from './chunks'
import { common } from './common'
import { content } from './content'
import { contentFaq } from './contentFaq'
import { documents } from './documents'
import { errors } from './errors'
import { indexGenerations } from './indexGenerations'
import { invitations } from './invitations'
import { jobs } from './jobs'
import { knowledgeBases } from './knowledgeBases'
import { members } from './members'
import { models } from './models'
import { retrieval } from './retrieval'
import { routes } from './routes'
import { settings } from './settings'
import { ui } from './ui'
import { workspaceReadiness } from './workspaceReadiness'
import { workspaces } from './workspaces'

type WidenValues<T> = {
  [K in keyof T]: T[K] extends object ? WidenValues<T[K]> : string
}

/**
 * 英文资源。结构必须与中文资源完全一致（satisfies 校验），
 * 保证 t() 的 key 在两种语言下都可用。
 */
export const en = {
  apiKeys,
  auth,
  chunks,
  common,
  content,
  contentFaq,
  documents,
  errors,
  indexGenerations,
  invitations,
  jobs,
  knowledgeBases,
  members,
  models,
  retrieval,
  routes,
  settings,
  workspaceReadiness,
  workspaces,
  ui,
} satisfies WidenValues<typeof zh>
