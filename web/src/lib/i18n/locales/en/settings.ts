import type { settings as zhSettings } from '../zh/settings'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const settings = {
  title: 'Settings',
  description: 'Adjust how the console looks and behaves in this browser.',
  nav: {
    appearance: 'Appearance',
    language: 'Language',
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
