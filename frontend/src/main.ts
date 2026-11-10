import { bootstrapApplication } from '@angular/platform-browser';
import { loadDesktopConnection } from './app/api/connection';
import { appConfig } from './app/app.config';
import { App } from './app/app';

// The desktop app connected to a server needs another transport.
loadDesktopConnection()
    .then(() => bootstrapApplication(App, appConfig))
    .catch((err) => console.error(err));
