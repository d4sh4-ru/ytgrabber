import { ApplicationConfig, isDevMode, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideServiceWorker } from '@angular/service-worker';
import { isWails } from './api/transport';

export const appConfig: ApplicationConfig = {
    providers: [
        provideBrowserGlobalErrorListeners(),
        // The web app's shell, for installing it on a phone. Not in the
        // desktop app: it ships its files itself.
        provideServiceWorker('ngsw-worker.js', {
            enabled: !isDevMode() && !isWails(),
            registrationStrategy: 'registerWhenStable:30000',
        }),
    ],
};
