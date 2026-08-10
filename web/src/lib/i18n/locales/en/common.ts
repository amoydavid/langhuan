import type { common as zhCommon } from '../zh/common'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const common = {
  // Brand name
  brandName: 'Langhuan',

  // Cross-page generic actions / states
  cancel: 'Cancel',
  loading: 'Loading…',
  sessionExpired: 'Your session has expired. Please sign in again.',
  forbidden: 'Insufficient permissions',
  invalidApiBaseUrl: 'VITE_API_BASE_URL must end with /api/v1',
  accountSettings: 'Account settings',
  appearanceSettings: 'Appearance settings',
  languageSettings: 'Language settings',
  signOut: 'Sign out',
  signOutDescription: 'You will need to sign in again to access the console.',
  openUserMenu: 'Open user menu',
  breadcrumbsAriaLabel: 'Breadcrumbs',
  languageSwitchAriaLabel: 'Switch language',

  // Command palette (CommandMenu)
  commandMenu: {
    searchPlaceholder: 'Search navigation, Workspaces, and knowledge bases…',
    noResults: 'No results found',
    knowledgeBases: 'Knowledge bases',
    quickActions: 'Quick actions',
    theme: 'Theme',
    createKnowledgeBase: 'Create knowledge base',
    uploadFileToKb: 'Upload files to "{{name}}"',
    createFaq: 'Create FAQ',
    openSearchTest: 'Open search test',
    light: 'Light',
    dark: 'Dark',
    system: 'System',
  },

  // Layout (sidebar / header)
  layout: {
    searchPlaceholder: 'Search anything',
    navOverview: 'Overview',
    navKnowledgeBases: 'Knowledge bases',
    navModels: 'Models',
    navMembers: 'Members',
    navSearchSettings: 'Search settings',
    navIntegrations: 'Integrations',
    navInvitations: 'Invitations',
    navApiKeys: 'API keys',
    navWorkspace: 'Workspace',
    navWorkspaceManagement: 'Workspace admin',
    navPlatformAdmin: 'Platform admin',
    navPlatformModels: 'Platform models',
    noWorkspace: 'No workspace yet',
    createWorkspacePrompt: 'Please create a workspace first',
    roleLabel: 'Role: {{role}}',
    createWorkspace: 'Create workspace',
  },
} satisfies Widen<typeof zhCommon>
