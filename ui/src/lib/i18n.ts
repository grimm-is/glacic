import { register, init, getLocaleFromNavigator } from 'svelte-i18n';

register('en', () => import('../locales/en.json'));
register('de', () => import('../locales/de.json'));

init({
    fallbackLocale: 'en',
    initialLocale: getLocaleFromNavigator(),
});
