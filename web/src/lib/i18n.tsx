import { createContext, createEffect, createSignal, onMount, useContext, type Accessor, type ParentProps } from "solid-js";
import { localeCookie, resolveLocale, savedLocale, translate, type Locale, type Parameters } from "./translation";

type I18n = { locale: Accessor<Locale>; setLocale: (locale: Locale) => void; t: (message: string, parameters?: Parameters) => string };
const I18nContext = createContext<I18n>();

export function I18nProvider(props: ParentProps) {
 // Each SSR request owns its locale. Hydration starts with the same default.
 const [locale, updateLocale] = createSignal<Locale>("zh-CN");
 const setLocale = (value: Locale) => {
  updateLocale(value);
  try { document.cookie = `${localeCookie}=${value}; Path=/; Max-Age=31536000; SameSite=Lax`; } catch { /* Switching still works when cookies are blocked. */ }
 };
 onMount(() => updateLocale(savedLocale(document.cookie) ?? resolveLocale(navigator.languages)));
 createEffect(() => { if (typeof document !== "undefined") document.documentElement.lang = locale(); });
 const value: I18n = { locale, setLocale, t: (message, parameters) => translate(locale(), message, parameters) };
 return <I18nContext.Provider value={value}>{props.children}</I18nContext.Provider>;
}

export function useI18n(): I18n {
 const value = useContext(I18nContext);
 if (!value) throw new Error("useI18n requires I18nProvider");
 return value;
}
