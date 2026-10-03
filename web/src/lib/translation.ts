import { en } from "./locales/en.ts";

export const locales = ["zh-CN", "en"] as const;
export type Locale = typeof locales[number];
export type Message = keyof typeof en;
export type Parameters = Record<string, string | number | undefined>;
export const localeCookie = "nulas-language";

export function resolveLocale(languages: readonly string[]): Locale {
 for (const language of languages) {
  if (/^zh(?:-|$)/i.test(language)) return "zh-CN";
  if (/^en(?:-|$)/i.test(language)) return "en";
 }
 return "zh-CN";
}

export function savedLocale(cookie: string): Locale | undefined {
 const value = cookie.split(";").map(part => part.trim()).find(part => part.startsWith(`${localeCookie}=`))?.slice(localeCookie.length + 1);
 return locales.find(locale => locale === value);
}

// Replace in one pass so parameter values cannot introduce more placeholders.
export function translate(locale: Locale, message: string, parameters: Parameters = {}): string {
 const template = locale === "en" && Object.hasOwn(en, message) ? en[message as Message] : message;
 return template.replace(/\{(\w+)\}/g, (placeholder, name: string) =>
  Object.hasOwn(parameters, name) ? String(parameters[name] ?? "") : placeholder);
}
