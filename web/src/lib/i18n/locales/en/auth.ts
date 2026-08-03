import type { auth as zhAuth } from '../zh/auth'

type Widen<T> = {
  [K in keyof T]: T[K] extends object ? Widen<T[K]> : string
}

export const auth = {
  layout: {
    brandName: 'Langhuan',
    tagline: 'Knowledge transformation and retrieval service',
  },
  signIn: {
    title: 'Sign in to Langhuan',
    description:
      'Enter the knowledge management console with the account assigned by your administrator.',
    emailLabel: 'Email',
    passwordLabel: 'Password',
    passwordPlaceholder: 'Enter your password',
    submitButton: 'Sign in',
    successToast: 'Signed in successfully',
    rateLimited: 'Too many sign-in attempts. Please try again later.',
  },
  setup: {
    emailLabel: 'Email',
    nicknameLabel: 'Nickname',
    passwordLabel: 'Password',
    confirmPasswordLabel: 'Confirm password',
    submitButton: 'Complete setup',
    successToast: 'Setup complete. Please sign in.',
  },
  invitationRegistration: {
    emailLabel: 'Email',
    nicknameLabel: 'Nickname',
    passwordLabel: 'Password',
    confirmPasswordLabel: 'Confirm password',
    submitButton: 'Accept invitation',
    successToast: 'Invitation accepted',
  },
  schemas: {
    invalidEmail: 'Please enter a valid email address',
    passwordRequired: 'Please enter a password',
    passwordMinLength: 'Password must be at least 8 characters',
    nicknameRequired: 'Please enter a nickname',
    confirmPasswordRequired: 'Please enter the password again',
    passwordMismatch: 'The passwords you entered do not match',
  },
} satisfies Widen<typeof zhAuth>
