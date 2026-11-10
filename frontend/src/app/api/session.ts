import type { components, paths } from './openapi';

// Users and devices exist only on ytgrabber-server; the types come from
// its OpenAPI spec (internal/server/openapi.json).

type Json<T> = T extends { content: { 'application/json': infer Body } } ? Body : never;

/** Who is signed in, and whether the server still has to be set up. */
export type SessionInfo = Json<paths['/session']['get']['responses'][200]>;

/** What a one-time code does when entered on the login screen. */
export type CodeCheck = Json<paths['/session/code']['post']['responses'][200]>;

export type CodeKind = CodeCheck['kind'];

export type SignInRequest = paths['/session']['post']['requestBody']['content']['application/json'];

/** A one-time code as the user types it: a device link or an invite. */
export type IssuedCode = Json<paths['/devices/link']['post']['responses'][200]>;

/** A token for a script, shown once. */
export type NewDevice = Json<paths['/devices']['post']['responses'][200]>;

export type Device = components['schemas']['model.Device'];

export type User = components['schemas']['model.User'];

export type UserRole = User['role'];

/** A user as the admin's list shows them. */
export type UserInfo = Json<paths['/users']['get']['responses'][200]>[number];
