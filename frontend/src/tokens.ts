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
};
export const font = '"Microsoft YaHei UI", "Segoe UI", sans-serif';
export function installTokens() {
  for (const [k, v] of Object.entries(colors))
    document.documentElement.style.setProperty(`--${k}`, v);
}
