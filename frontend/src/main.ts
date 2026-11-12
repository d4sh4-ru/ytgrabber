import { bootstrapApplication } from '@angular/platform-browser';
import { loadDesktopConnection } from './app/api/connection';
import { appConfig } from './app/app.config';
import { App } from './app/app';

// navigator.standalone exists only on iOS: true when opened from the home
// screen, where the layout keeps clear of the system's top blur.
if ((navigator as Navigator & { standalone?: boolean }).standalone === true) {
    document.documentElement.classList.add('ios-standalone');
}

// The desktop app connected to a server needs another transport.
loadDesktopConnection()
    .then(() => bootstrapApplication(App, appConfig))
    .catch((err) => console.error(err));
