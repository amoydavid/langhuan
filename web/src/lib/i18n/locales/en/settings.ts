import type { settings as zhSettings } from '../zh/settings'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const settings = {
  title: 'Settings',
  description: 'Adjust how the console looks and behaves in this browser.',
  nav: {
    account: 'Account',
    appearance: 'Appearance',
    language: 'Language',
  },
  account: {
    passwordTitle: 'Change password',
    passwordDescription:
      'Update your sign-in password. Sessions on other devices are unaffected.',
    oldPassword: 'Current password',
    oldPasswordPlaceholder: 'Enter your current password',
    newPassword: 'New password',
    newPasswordPlaceholder: 'At least 8 characters',
    confirmPassword: 'Confirm new password',
    confirmPasswordPlaceholder: 'Re-enter the new password',
    changePasswordSubmit: 'Update password',
    passwordChanged: 'Password updated',
    wrongOldPassword: 'Current password is incorrect',
    ssoTitle: 'Enterprise SSO',
    ssoDescription: 'Manage bound external identity providers.',
    ssoDisabled: 'OIDC is not enabled; external identities cannot be bound.',
    loadingIdentities: 'Loading…',
    noIdentities: 'No external identities bound yet.',
    bindSSO: 'Bind enterprise SSO',
    bindRedirecting: 'Redirecting to identity provider…',
  },
  appearance: {
    title: 'Theme',
    description:
      'The sidebar always stays dark; the content area follows the theme.',
    pageDescription:
      'Choose the light or dark theme of the console. The preference is saved only in this browser.',
    options: {
      light: 'Light',
      lightDescription: 'Dark sidebar with a cool white canvas',
      dark: 'Dark',
      darkDescription: 'Dark green canvas with a deeper sidebar',
      system: 'System',
      systemDescription: 'Automatically match the device light/dark setting',
    },
    submit: 'Save theme',
    saved: 'Theme preference updated',
  },
  language: {
    title: 'Language',
    description: 'Choose the display language of the console.',
    label: 'Interface language',
    saved: 'Language preference updated',
  },
} satisfies Widen<typeof zhSettings>
