import { SignInFailure } from './sign-in-api';

/** A message shown above the sign-in form, or in place of it when the sign-in can't go on. */
export interface Notice {
  tone: 'error' | 'info';
  title: string;
  body: string;
}

const tryAgain = 'Try again, or sign in another way.';

/** The `error` a provider sign-in returns with, when the person can try another way. */
const providerErrors: Record<string, Notice> = {
  cancelled: {
    tone: 'info',
    title: 'Sign-in cancelled',
    body: `You cancelled at the provider. ${tryAgain}`,
  },
  provider_failed: {
    tone: 'error',
    title: "The provider couldn't sign you in",
    body: tryAgain,
  },
  not_admitted: {
    tone: 'error',
    title: "This account isn't allowed here",
    body: 'The provider account is outside the limits set for this installation. Sign in another way, or ask an owner.',
  },
  no_membership: {
    tone: 'error',
    title: 'No organisation to sign in to',
    body: "You don't belong to an organisation on this installation yet. Ask an owner to add you.",
  },
  account_exists: {
    tone: 'error',
    title: 'You already have an account',
    body: 'An account with this email signs in another way. Sign in that way first, then link this provider.',
  },
  account_disabled: {
    tone: 'error',
    title: 'This account is disabled',
    body: 'Ask an owner to turn it back on.',
  },
};

const somethingWentWrong: Notice = {
  tone: 'error',
  title: 'Something went wrong',
  body: tryAgain,
};

export const expired: Notice = {
  tone: 'error',
  title: 'This sign-in has timed out',
  body: 'Sign-ins last 15 minutes and finish in the browser that started them. Go back to the application you were using and start again.',
};

/** Shown when the page is opened without a sign-in to continue. */
export const nothingToContinue: Notice = {
  tone: 'info',
  title: 'Open the application you want to use',
  body: 'It sends you here when it needs you to sign in.',
};

export const signedOut: Notice = {
  tone: 'info',
  title: "You've signed out",
  body: 'Open the application you want to use to sign in again.',
};

export const noMethods: Notice = {
  tone: 'info',
  title: 'No way to sign in is set up',
  body: 'Ask the person who runs this installation to turn on passwords or add a provider.',
};

/** A provider sign-in for someone with no account, which only the managed cloud can sign up. */
export const noAccount: Notice = {
  tone: 'error',
  title: 'No account for this sign-in',
  body: 'Ask an owner to add you, or sign in another way.',
};

export function providerNotice(error: string): Notice {
  return providerErrors[error] ?? somethingWentWrong;
}

/** The notice for a failed `/v1/auth` step that the person can recover from on this page. */
export function failureNotice(failure: SignInFailure): Notice {
  switch (failure) {
    case 'expired':
      return expired;
    case 'credentials':
      return {
        tone: 'error',
        title: "That didn't work",
        body: 'Check your email and password and try again.',
      };
    case 'forbidden':
      return {
        tone: 'error',
        title: "This account can't sign in here",
        body: 'Ask an owner of the organisation for access.',
      };
    case 'invalid':
      return {
        tone: 'error',
        title: 'Check what you entered',
        body: 'Your email or password is not valid.',
      };
    case 'unavailable':
      return {
        tone: 'error',
        title: "Ivaldi can't be reached",
        body: 'Check your connection and try again in a moment.',
      };
    default:
      return somethingWentWrong;
  }
}
