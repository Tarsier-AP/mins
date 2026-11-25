// Queue and option state, persisted as JSON in the OS config directory so a
// restart keeps the batch and the flags the user picked.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

#[derive(Debug, Default, Serialize, Deserialize)]
pub struct State {
    #[serde(default)]
    pub files: Vec<PathBuf>,
    #[serde(default)]
    pub values: BTreeMap<String, String>,
    /// True while a batch was running, so the next launch can say "interrupted".
    #[serde(default)]
    pub running: bool,
}

impl State {
    /// A missing, unreadable or corrupt file means "start empty", never a crash.
    pub fn load(path: &Path) -> State {
        std::fs::read(path)
            .ok()
            .and_then(|b| serde_json::from_slice(&b).ok())
            .unwrap_or_default()
    }

    // shortcut: non-UTF8 paths do not survive JSON; switch to a binary format
    // if that ever matters.
    pub fn save(&self, path: &Path) {
        if let Some(dir) = path.parent() {
            let _ = std::fs::create_dir_all(dir);
        }
        if let Ok(b) = serde_json::to_vec_pretty(self) {
            let _ = std::fs::write(path, b);
        }
    }
}

/// `~/.config/mins/gui/state.json` and the platform equivalents.
pub fn default_path() -> Option<PathBuf> {
    config_dir().map(|d| d.join("state.json"))
}

fn config_dir() -> Option<PathBuf> {
    #[cfg(target_os = "windows")]
    let base = std::env::var_os("APPDATA").map(PathBuf::from);
    #[cfg(target_os = "macos")]
    let base = std::env::var_os("HOME")
        .map(|h| PathBuf::from(h).join("Library").join("Application Support"));
    #[cfg(not(any(target_os = "windows", target_os = "macos")))]
    let base = std::env::var_os("XDG_CONFIG_HOME")
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".config")));
    base.map(|b| b.join("mins").join("gui"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_files_values_and_running() {
        let dir = std::env::temp_dir().join(format!("mins-gui-store-{}", std::process::id()));
        let path = dir.join("state.json");
        let mut st = State::default();
        st.files = vec![PathBuf::from("/tmp/a.mp4")];
        st.values.insert("target".into(), "92".into());
        st.running = true;
        st.save(&path);

        let back = State::load(&path);
        assert_eq!(back.files, st.files);
        assert_eq!(back.values.get("target").map(String::as_str), Some("92"));
        assert!(back.running);

        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn missing_or_corrupt_state_is_empty() {
        let dir = std::env::temp_dir().join(format!("mins-gui-store-bad-{}", std::process::id()));
        let path = dir.join("state.json");
        assert!(State::load(&path).files.is_empty());

        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(&path, b"{not json").unwrap();
        let st = State::load(&path);
        assert!(st.files.is_empty() && st.values.is_empty() && !st.running);

        let _ = std::fs::remove_dir_all(&dir);
    }
}
