/**
 * The source catalog. English is the source language (ADR 0019); every other locale
 * is a localization resource typed against these keys, so a missing key is a compile
 * error rather than a runtime blank.
 *
 * `error.*` keys mirror the API's stable error codes. Codes the catalog does not cover
 * fall back to the server's English message, which is why partial coverage is safe.
 */
export const en = {
  'app.title': 'ProbeHive',
  'app.signOut': 'Sign out',
  'app.signingOut': 'Signing out…',
  'app.language': 'Language',

  'auth.checkingSession': 'Checking session…',

  'setup.heading': 'Set up ProbeHive',
  'setup.intro':
    'Create the first administrator account for this installation. Its Organization is created at the same time, so you can add a Monitor immediately.',
  'setup.form': 'Create first administrator',
  'setup.email': 'Email',
  'setup.displayName': 'Display name',
  'setup.password': 'Password',
  'setup.submit': 'Create administrator',
  'setup.submitting': 'Creating…',
  'setup.alreadyComplete': 'Setup is already completed on this installation.',
  'setup.signInInstead': 'Sign in instead.',

  'login.heading': 'Sign in',
  'login.form': 'Sign in',
  'login.email': 'Email',
  'login.password': 'Password',
  'login.submit': 'Sign in',
  'login.submitting': 'Signing in…',
  'login.invalid': 'Invalid email or password.',
  'login.rateLimited': 'Too many attempts; wait a minute and try again.',
  'login.failed': 'Signing in failed. Try again.',

  'organizations.heading': 'Organizations',
  'organizations.loading': 'Loading Organizations…',
  'organizations.empty': 'No Organizations yet.',

  'organization.create.heading': 'Create an Organization',
  'organization.create.form': 'Create organization',
  'organization.create.slug': 'Slug',
  'organization.create.displayName': 'Display name',
  'organization.create.submit': 'Create',
  'organization.create.submitting': 'Creating…',
  'organization.create.conflict':
    'That slug is already in use by an Organization with a different display name.',
  'organization.create.created': 'Organization created.',
  'organization.create.replayed': 'Organization already existed; returning it.',
  'organization.create.view': 'View {name}',

  'organization.loading': 'Loading…',
  'organization.notFound': 'This Organization does not exist.',
  'organization.loadFailed': 'The Organization could not be loaded.',
  'organization.slug': 'Slug',
  'organization.identifier': 'Identifier',
  'organization.created': 'Created',
  'organization.defaultProject': 'Default Project',
  'organization.name': 'Name',

  'error.user.email.invalid':
    "An email address contains one '@' with non-empty sides, no whitespace, and at most 254 characters.",
  'error.user.displayName.invalid': 'A display name is 1 to 100 characters after trimming.',
  'error.user.password.length': 'A password is 12 to 128 characters.',
  'error.user.credentials.invalid': 'The email and password combination did not match a local account.',
  'error.user.setup.alreadyCompleted': 'This installation already has a user; sign in instead.',
  'error.organization.slug.invalid':
    'A slug is 3 to 63 characters of lowercase letters, digits, and single interior hyphens, starting and ending with a letter or digit.',
  'error.organization.displayName.invalid': 'A display name is 1 to 100 characters after trimming.',
  'error.organization.slug.conflict':
    'An Organization with that slug already exists with a different display name.',
  'error.auth.unauthorized': 'Sign in to continue.',
  'error.auth.forbidden': 'This account is not allowed to perform that action.',
  'error.resource.notFound': 'That resource does not exist.',
  'error.request.rateLimited': 'Too many attempts; wait a minute and try again.',
  'error.request.antiforgery.invalid': 'The page went stale; reload and try again.',
  'error.request.origin.rejected': 'The request came from an unexpected origin.',
  'error.request.malformed': 'The request could not be understood.',
  'error.server.internalError': 'Something went wrong. Try again.',
} as const

export type MessageKey = keyof typeof en

/** A locale catalog must define every key the source catalog defines. */
export type Catalog = Record<MessageKey, string>
