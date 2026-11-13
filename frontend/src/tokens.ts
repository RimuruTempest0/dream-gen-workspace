export const colors = {
  primary: "#245BDB",
  surface: "#FFFFFF",
  background: "#F5F6F8",
  text: "#182230",
  success: "#18794E",
  successBg: "#EAF6EF",
  dangerBg: "#FFF1EF",
  warningBg: "#FFF6E5",
  danger: "#B42318",
  muted: "#586779",
  border: "#DCE2EA",
  soft: "#EDF2FA",
  warning: "#8B5B08",
  scrollbar: "#9DAABC",
  // Concept stage (0.7.18): the app's only dark "gallery wall". ink is the same
  // value as text; inkAccent is the primary tint used for selection and focus on ink.
  ink: "#182230",
  inkRaised: "#1F2B3B",
  inkSelected: "#1E3054",
  inkLine: "#3A4A60",
  inkText: "#F2F4F7",
  inkMuted: "#A7B2C2",
  inkAccent: "#8FB0F5",
};
export const font = '"Microsoft YaHei UI", "Segoe UI", sans-serif';
export function installTokens() {
  for (const [k, v] of Object.entries(colors))
    document.documentElement.style.setProperty(`--${k}`, v);
}
