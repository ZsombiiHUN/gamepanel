import "i18next";
import type en from "./locales/en/translation.json";

// Keys are checked at compile time against the English file.
declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation";
    resources: { translation: typeof en };
    returnNull: false;
  }
}
