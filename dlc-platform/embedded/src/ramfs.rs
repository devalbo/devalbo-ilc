//! A filesystem that lives in RAM — **the badge's storage, for now** (D5).
//!
//! # Why a real one rather than a stub
//!
//! The badge granted no filesystem at all: `get_directories` returned nothing,
//! every descriptor method answered `ACCESS`, and the comment said that was
//! honest until D5. It was honest, and it also meant an app that stores anything
//! could not run here. tictactoe keeps its whole game in `game.json` (§7.1 — the
//! file is the truth), so on hardware it answered `state` from a fresh board and
//! failed every `play` with `mkdir /: errno 2`. The engine, the rules and the
//! wire format were all fine; there was nowhere to put a file.
//!
//! # What this is NOT
//!
//! Persistent. Everything here dies with the power, which is Phase 4's problem
//! (FAT over the 16 MB flash, per D11) and deliberately not this file's. Naming
//! that plainly matters more than it sounds: an app cannot tell the difference
//! from inside, so the *world* has to be the thing that says so.
//!
//! Also not: symlinks, hard links, permissions, timestamps that advance, or
//! directories that cost anything. A flat `Vec` of paths is the whole structure.
//! With one app writing one file, a tree would be machinery for a case nobody
//! has — and D5's claim is that a capability can be real without being complete.
//!
//! # The shape that matters
//!
//! A file's bytes live in a [`SharedBuffer`], the same primitive stdout uses, so
//! a stream handed to the guest writes into the storage the filesystem reads.
//! Two descriptors on one path see one file because they hold clones of one
//! buffer, rather than because anything keeps copies in step.

use alloc::boxed::Box;
use alloc::string::{String, ToString};
use alloc::vec::Vec;

use bytes::Bytes;
use wasmtime_wasi_io::poll::Pollable;
use wasmtime_wasi_io::streams::{
    DynInputStream, DynOutputStream, InputStream, OutputStream, StreamError, StreamResult,
};

use crate::uart::SharedBuffer;

/// What a path is.
#[derive(Clone)]
enum Kind {
    Dir,
    /// SHARED WITH EVERY DESCRIPTOR OPEN ON IT, which is what makes a write
    /// through a stream visible to a later read.
    File(SharedBuffer),
}

struct Entry {
    /// Absolute and normalised: always starts `/`, never ends in one except the
    /// root itself.
    path: String,
    kind: Kind,
}

/// The whole filesystem.
pub struct RamFs {
    entries: Vec<Entry>,
}

impl Default for RamFs {
    fn default() -> Self {
        Self::new()
    }
}

impl RamFs {
    pub fn new() -> Self {
        // THE ROOT EXISTS FROM THE START. `os.MkdirAll` walks up to `/` and
        // stats it; a root that had to be created would fail the first write
        // with the very error this file exists to remove.
        Self {
            entries: alloc::vec![Entry { path: "/".to_string(), kind: Kind::Dir }],
        }
    }

    /// Resolve `path` relative to `base` into a normalised absolute path.
    ///
    /// HANDLES `.` AND `..` BY REMOVING THEM rather than by refusing. A guest
    /// that composes `a/../b` is not attacking anything — there is no wider
    /// filesystem to escape into, since this one has no outside.
    fn absolute(base: &str, path: &str) -> String {
        let mut parts: Vec<&str> = Vec::new();
        let joined = if path.starts_with('/') {
            path.to_string()
        } else if base == "/" {
            alloc::format!("/{path}")
        } else {
            alloc::format!("{base}/{path}")
        };
        for part in joined.split('/') {
            match part {
                "" | "." => {}
                ".." => {
                    parts.pop();
                }
                other => parts.push(other),
            }
        }
        if parts.is_empty() {
            return "/".to_string();
        }
        let mut out = String::new();
        for part in parts {
            out.push('/');
            out.push_str(part);
        }
        out
    }

    fn find(&self, path: &str) -> Option<&Entry> {
        self.entries.iter().find(|e| e.path == path)
    }

    pub fn is_dir(&self, path: &str) -> bool {
        matches!(self.find(path).map(|e| &e.kind), Some(Kind::Dir))
    }

    pub fn exists(&self, path: &str) -> bool {
        self.find(path).is_some()
    }

    /// The bytes behind a path, if it is a file.
    pub fn file(&self, path: &str) -> Option<SharedBuffer> {
        match self.find(path).map(|e| &e.kind) {
            Some(Kind::File(buffer)) => Some(buffer.clone()),
            _ => None,
        }
    }

    /// Create a directory. Idempotent on an existing one.
    ///
    /// WASI says a directory that already exists is `EXIST`, and `MkdirAll`
    /// tolerates that — but it also stats first, so the common path never gets
    /// here. Returning the error faithfully costs nothing and keeps a
    /// hand-written `mkdir` behaving the way its author expects.
    pub fn make_dir(&mut self, path: &str) -> Result<(), Existing> {
        if let Some(entry) = self.find(path) {
            return match entry.kind {
                Kind::Dir => Err(Existing::Dir),
                Kind::File(_) => Err(Existing::File),
            };
        }
        self.entries.push(Entry { path: path.to_string(), kind: Kind::Dir });
        Ok(())
    }

    /// Create a file, or return the one already there.
    pub fn make_file(&mut self, path: &str) -> Option<SharedBuffer> {
        if let Some(entry) = self.find(path) {
            return match &entry.kind {
                Kind::File(buffer) => Some(buffer.clone()),
                Kind::Dir => None,
            };
        }
        let buffer = SharedBuffer::default();
        self.entries.push(Entry { path: path.to_string(), kind: Kind::File(buffer.clone()) });
        Some(buffer)
    }

    pub fn remove(&mut self, path: &str) -> bool {
        let before = self.entries.len();
        self.entries.retain(|e| e.path != path);
        self.entries.len() != before
    }

    /// The immediate children of a directory, as (name, is_dir).
    pub fn children(&self, dir: &str) -> Vec<(String, bool)> {
        let prefix = if dir == "/" { "/".to_string() } else { alloc::format!("{dir}/") };
        let mut out = Vec::new();
        for entry in &self.entries {
            if entry.path == dir || !entry.path.starts_with(&prefix) {
                continue;
            }
            let rest = &entry.path[prefix.len()..];
            // IMMEDIATE children only: anything with a separator left belongs to
            // a subdirectory, and listing it here would report a nested file as
            // a sibling.
            if rest.contains('/') {
                continue;
            }
            out.push((rest.to_string(), matches!(entry.kind, Kind::Dir)));
        }
        out
    }
}

/// What was already at a path a `mkdir` wanted.
pub enum Existing {
    Dir,
    File,
}

/// A descriptor the guest holds.
///
/// A PATH, not a pointer into the store. The alternative — an index — goes stale
/// the moment anything is removed, and a stale index is a descriptor that
/// silently reads someone else's file.
pub struct Node {
    pub path: String,
    pub dir: bool,
}

impl Node {
    pub fn root() -> Self {
        Self { path: "/".to_string(), dir: true }
    }

    /// Resolve a path given relative to this descriptor.
    pub fn resolve(&self, path: &str) -> String {
        RamFs::absolute(&self.path, path)
    }
}

/// An open directory being read.
pub struct DirStream {
    pub remaining: Vec<(String, bool)>,
}

// ---------------------------------------------------------------------------
// Streams over a file
// ---------------------------------------------------------------------------

/// Writes into a file's buffer.
///
/// # Why not `SinkStream<SharedBuffer>`
///
/// Because `ByteSink for SharedBuffer` fires the stdout ECHO hook — the one that
/// makes a guest's `println` appear on the panel as it happens. Reusing it here
/// would paint the contents of every file the app writes onto the badge's
/// screen and into the boot log. The buffer is the right primitive; the sink
/// around it is not.
pub struct FileSink(pub SharedBuffer);

#[async_trait::async_trait]
impl Pollable for FileSink {
    async fn ready(&mut self) {}
}

impl OutputStream for FileSink {
    fn write(&mut self, bytes: Bytes) -> StreamResult<()> {
        self.0.append(&bytes);
        Ok(())
    }
    fn flush(&mut self) -> StreamResult<()> {
        Ok(())
    }
    fn check_write(&mut self) -> StreamResult<usize> {
        Ok(4096)
    }
}

/// Reads a file's bytes from a fixed offset onward.
///
/// A SNAPSHOT, taken when the stream is created. A guest that reads a file while
/// something else writes it gets what was there when it opened — which on a
/// single-threaded host with one app is not a race anyone can observe, and is
/// the behaviour a reader would expect if it were.
pub struct FileSource {
    bytes: Vec<u8>,
    at: usize,
}

impl FileSource {
    pub fn new(buffer: &SharedBuffer, offset: u64) -> Self {
        let bytes = buffer.snapshot();
        let at = (offset as usize).min(bytes.len());
        Self { bytes, at }
    }
}

#[async_trait::async_trait]
impl Pollable for FileSource {
    async fn ready(&mut self) {}
}

impl InputStream for FileSource {
    fn read(&mut self, size: usize) -> StreamResult<Bytes> {
        if self.at >= self.bytes.len() {
            // CLOSED, NOT EMPTY. An empty read means "nothing right now, ask
            // again"; end-of-file means there will never be more, and a reader
            // told the first thing spins forever on the second.
            return Err(StreamError::Closed);
        }
        let end = (self.at + size).min(self.bytes.len());
        let chunk = Bytes::copy_from_slice(&self.bytes[self.at..end]);
        self.at = end;
        Ok(chunk)
    }
}

pub fn boxed_file_sink(buffer: SharedBuffer) -> DynOutputStream {
    Box::new(FileSink(buffer))
}

pub fn boxed_file_source(buffer: &SharedBuffer, offset: u64) -> DynInputStream {
    Box::new(FileSource::new(buffer, offset))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn paths_are_normalised_against_their_base() {
        assert_eq!(RamFs::absolute("/", "game.json"), "/game.json");
        assert_eq!(RamFs::absolute("/", "/game.json"), "/game.json");
        assert_eq!(RamFs::absolute("/data", "game.json"), "/data/game.json");
        // `.` and `..` collapse rather than being refused — there is no outside
        // to escape to.
        assert_eq!(RamFs::absolute("/", "./a/../b"), "/b");
        assert_eq!(RamFs::absolute("/", ".."), "/");
        assert_eq!(RamFs::absolute("/a/b", ".."), "/a");
    }

    #[test]
    fn a_written_file_reads_back() {
        let mut fs = RamFs::new();
        let buffer = fs.make_file("/game.json").expect("a fresh path is a file");
        buffer.append(b"{\"board\":[]}");
        assert_eq!(fs.file("/game.json").unwrap().snapshot(), b"{\"board\":[]}");
    }

    /// THE PROPERTY THE WHOLE DESIGN RESTS ON: two descriptors on one path are
    /// one file, because they share a buffer rather than because anything keeps
    /// copies in step.
    #[test]
    fn two_handles_on_one_path_are_one_file() {
        let mut fs = RamFs::new();
        let a = fs.make_file("/x").unwrap();
        let b = fs.make_file("/x").unwrap();
        a.append(b"hello");
        assert_eq!(b.snapshot(), b"hello");
    }

    #[test]
    fn the_root_exists_before_anything_is_created() {
        // `os.MkdirAll` stats its way up to `/`, and a root that had to be
        // created would fail the first write with the error D5 exists to remove.
        let fs = RamFs::new();
        assert!(fs.is_dir("/"));
        assert!(fs.exists("/"));
    }

    #[test]
    fn a_directory_lists_only_its_immediate_children() {
        let mut fs = RamFs::new();
        fs.make_file("/a").unwrap();
        let _ = fs.make_dir("/sub");
        fs.make_file("/sub/deep").unwrap();

        let mut top: Vec<String> = fs.children("/").into_iter().map(|(n, _)| n).collect();
        top.sort();
        assert_eq!(top, alloc::vec!["a".to_string(), "sub".to_string()]);

        // NOT the nested file — reporting it at the top would make a subdirectory
        // invisible and its contents look like siblings.
        let sub: Vec<String> = fs.children("/sub").into_iter().map(|(n, _)| n).collect();
        assert_eq!(sub, alloc::vec!["deep".to_string()]);
    }

    #[test]
    fn a_removed_file_is_gone() {
        let mut fs = RamFs::new();
        fs.make_file("/x").unwrap();
        assert!(fs.remove("/x"));
        assert!(!fs.exists("/x"));
        assert!(!fs.remove("/x"));
    }

    /// Reading past the end is CLOSED rather than an empty chunk: a reader told
    /// "nothing right now" asks again forever.
    #[test]
    fn a_source_ends_rather_than_starving() {
        let buffer = SharedBuffer::default();
        buffer.append(b"abc");
        let mut source = FileSource::new(&buffer, 0);
        assert_eq!(&source.read(2).unwrap()[..], b"ab");
        assert_eq!(&source.read(2).unwrap()[..], b"c");
        assert!(matches!(source.read(2), Err(StreamError::Closed)));
    }

    #[test]
    fn a_source_starts_at_its_offset() {
        let buffer = SharedBuffer::default();
        buffer.append(b"abcdef");
        let mut source = FileSource::new(&buffer, 3);
        assert_eq!(&source.read(10).unwrap()[..], b"def");
    }
}
