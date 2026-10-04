import { createContext, createEffect, createSignal, onMount, useContext, type Accessor, type ParentProps } from "solid-js";
import { localeCookie, resolveLocale, savedLocale, translate, type Locale, type Parameters } from "./translation";

import { api } from "./api";

type I18n = { locale: Accessor<Locale>; setLocale: (locale: Locale) => void; t: (message: string, parameters?: Parameters) => string };
const I18nContext = createContext<I18n>();

export function I18nProvider(props: ParentProps) {
 // Each SSR request owns its locale. Hydration starts with the same default.
 const [locale, updateLocale] = createSignal<Locale>("zh-CN");
 // Serialize updates so rapid switches reach the shared tray in selection order.
 let trayLanguageUpdate = Promise.resolve();
 const syncTrayLanguage = (language: Locale) => {
  trayLanguageUpdate = trayLanguageUpdate.then(async () => {
   try { await api("runtime/tray", { method: "PUT", headers: {"Content-Type": "application/json"}, body: JSON.stringify({language}) }); }
   catch (error) { console.warn("Unable to synchronize tray language", error); }
  });
 };
 const setLocale = (value: Locale) => {
  updateLocale(value);
  syncTrayLanguage(value);
  try { document.cookie = `${localeCookie}=${value}; Path=/; Max-Age=31536000; SameSite=Lax`; } catch { /* Switching still works when cookies are blocked. */ }
 };
 onMount(() => {
  const value = savedLocale(document.cookie) ?? resolveLocale(navigator.languages);
  updateLocale(value);
  syncTrayLanguage(value);
 });
 createEffect(() => { if (typeof document !== "undefined") document.documentElement.lang = locale(); });
 const value: I18n = { locale, setLocale, t: (message, parameters) => translate(locale(), message, parameters) };
 return <I18nContext.Provider value={value}>{props.children}</I18nContext.Provider>;
}

export function useI18n(): I18n {
 const value = useContext(I18nContext);
 if (!value) throw new Error("useI18n requires I18nProvider");
 return value;
}
