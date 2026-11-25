// The engine's option surface (`mins --describe`) plus the plain-language copy
// that the GUI shows. Ranges and enum values come from Go; the words live here.

use std::path::Path;
use std::process::Command;

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct FlagDesc {
    pub name: String,
    pub kind: String,
    #[serde(default)]
    pub default: String,
    #[serde(default)]
    pub values: Vec<String>,
    #[serde(default)]
    pub min: Option<f64>,
    #[serde(default)]
    pub max: Option<f64>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Surface {
    pub version: String,
    pub flags: Vec<FlagDesc>,
}

/// Runs `mins --describe`. None means the engine could not be asked, and the
/// GUI then offers only the controls it can hardcode.
pub fn describe(bin: &Path) -> Option<Surface> {
    let out = Command::new(bin).arg("--describe").output().ok()?;
    if !out.status.success() {
        return None;
    }
    serde_json::from_slice(&out.stdout).ok()
}

/// (flag, label, one-sentence help). Every described flag needs an entry; the
/// test below enforces it so a new engine flag cannot ship without words.
pub const COPY: &[(&str, &str, &str)] = &[
    ("mode", "Mode", "What kind of file to process. Auto picks by file extension."),
    ("encoder", "Video encoder", "Which AV1 encoder to use. SVT is the fastest on most machines."),
    ("vid-fmt", "Video container", "File type for the finished video. MP4 plays almost everywhere."),
    ("acodec", "Audio codec", "How to store the sound in a video. Copy keeps the original audio untouched."),
    ("img-fmt", "Image format", "File type for still images."),
    ("img-encoder", "Image encoder", "Which encoder writes the image. Auto matches the format."),
    ("avif-yuv", "AVIF color sampling", "How much color detail AVIF keeps. 420 is the safe default."),
    ("crf-min", "Lowest video quality number", "Lower bound for the search. Empty means the encoder's default."),
    ("crf-max", "Highest video quality number", "Upper bound for the search. Empty means the encoder's default."),
    ("q-min", "Lowest image quality", "Lower bound of the image quality search."),
    ("q-max", "Highest image quality", "Upper bound of the image quality search."),
    ("q-auto", "Learn quality across files", "Reuse what earlier files taught the search. Faster on similar batches."),
    ("br-min", "Lowest audio bitrate (kbps)", "Lower bound when searching audio bitrate."),
    ("br-max", "Highest audio bitrate (kbps)", "Upper bound when searching audio bitrate."),
    ("audio-metric", "Audio quality metric", "How audio quality is measured. ViSQOL is closest to human hearing."),
    ("target-audio", "Audio quality target", "Minimum audio score to accept. Empty means the metric's default."),
    ("audio-search", "Search audio bitrate", "Try bitrates until the audio just passes, instead of a fixed one."),
    ("audio-band", "Audio band (Hz)", "Frequency range compared when scoring audio, as LOW:HIGH."),
    ("visqol-model", "ViSQOL model file", "Optional replacement model for ViSQOL. The bundled one is used by default."),
    ("audio-fmt", "Audio container", "File type for audio-only output. Auto keeps the source's container."),
    ("method", "WebP effort", "Higher is slower and smaller. 6 is the maximum."),
    ("aspeed", "AVIF speed", "Higher is faster and larger. 0 is the slowest and smallest."),
    ("jxl-effort", "JXL effort", "Higher is slower and smaller. 9 is the maximum."),
    ("anim-fps", "Animation frame rate", "Frames per second kept from the source video."),
    ("anim-width", "Animation width", "Scale frames to this width. Empty keeps the source size."),
    ("target", "Quality target (VMAF)", "Minimum quality score to accept, 0-100. Higher looks better and is larger."),
    ("start", "Trim start (seconds)", "Skip the beginning of the video."),
    ("end", "Trim end (seconds)", "Stop before this point in the video."),
    ("atracks", "Audio tracks", "Which audio tracks to keep: all, none, or numbers or languages."),
    ("stracks", "Subtitle tracks", "Which subtitle tracks to keep: all, none, or numbers or languages."),
    ("fbf", "Frame-by-frame quality check", "Check every frame instead of the average. Slower, catches bad moments."),
    ("instances", "Parallel workers", "How many files to process at once."),
    ("fbf-chunk-seconds", "Frame chunk length (seconds)", "How much video the frame-by-frame check keeps on disk at a time."),
    ("refine", "Repair weak moments", "Re-encode short fragments that fall below the target."),
    ("refine-seconds", "Repair fragment length (seconds)", "Longest fragment the repair step will touch."),
    ("refine-min-frames", "Shortest repair fragment (frames)", "Fragments shorter than this are left alone."),
    ("refine-step", "Repair step", "How much to lower the quality number on each repair attempt."),
    ("refine-boundary-cost", "Repair boundary cost (frames)", "How strongly the repair avoids extra cuts. 0 ignores the cost."),
    ("no-json", "Skip JSON reports", "Don't write the .vmaf.json report next to the output."),
    ("skip-existing", "Skip finished files", "Leave files alone when their output already exists."),
    ("log", "Run log file", "Also write a plain-text log of the run here."),
    ("out-dir", "Output folder", "Put finished files and reports here instead of next to each input."),
];

pub fn label(name: &str) -> &str {
    COPY.iter()
        .find(|(n, _, _)| *n == name)
        .map(|(_, label, _)| *label)
        .unwrap_or(name)
}

pub fn help(name: &str) -> &str {
    COPY.iter()
        .find(|(n, _, _)| *n == name)
        .map(|(_, _, help)| *help)
        .unwrap_or("")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn copy_covers_every_described_flag() {
        let Some(surface) = describe(&crate::mins_bin()) else {
            eprintln!("skipping: cannot run mins --describe");
            return;
        };
        for f in &surface.flags {
            assert!(
                COPY.iter().any(|(n, _, _)| *n == f.name),
                "no GUI copy for --{} (add it to surface::COPY)",
                f.name
            );
        }
        for (name, label, help) in COPY {
            assert!(
                surface.flags.iter().any(|f| f.name == *name),
                "GUI copy for --{} but the engine no longer describes it",
                name
            );
            assert!(!label.is_empty() && !help.is_empty(), "--{} has empty copy", name);
        }
    }
}
