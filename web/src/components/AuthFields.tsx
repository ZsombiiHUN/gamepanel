import { Field } from "./ui";
import { useTranslation } from "react-i18next";

/**
 * An account password input. "current" is a password being checked; "new" is
 * one being chosen and carries the panel's length rule (12 to 256).
 */
export function PasswordField({
  label,
  kind = "current",
  value,
  onChange,
  hint,
  invalid,
  readOnly,
  autoFocus,
}: {
  label: string;
  kind?: "current" | "new";
  value: string;
  onChange: (value: string) => void;
  hint?: string;
  invalid?: boolean;
  readOnly?: boolean;
  autoFocus?: boolean;
}) {
  return (
    <Field label={label} hint={hint}>
      {(id) => (
        <input
          id={id}
          className="input"
          type="password"
          autoComplete={kind === "new" ? "new-password" : "current-password"}
          required
          minLength={kind === "new" ? 12 : undefined}
          maxLength={256}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={invalid || undefined}
          readOnly={readOnly}
          autoFocus={autoFocus}
        />
      )}
    </Field>
  );
}

/** The second factor asked for before a sensitive change: an authenticator or a recovery code. */
export function MfaCodeField({
  value,
  onChange,
  fullWidth = false,
}: {
  value: string;
  onChange: (value: string) => void;
  fullWidth?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <Field label={t("authFields.authenticatorOrRecoveryCode")}>
      {(id) => (
        <input
          id={id}
          className={`input font-mono ${fullWidth ? "" : "w-48"}`}
          autoComplete="one-time-code"
          required
          maxLength={64}
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
    </Field>
  );
}
