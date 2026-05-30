//! WIT kebab-case → emitted camelCase / PascalCase.

/// `console-io` → `consoleIo`, `read-line` → `readLine`
pub fn to_camel_case(kebab: &str) -> String {
    let mut out = String::new();
    for (i, segment) in kebab.split('-').enumerate() {
        if segment.is_empty() {
            continue;
        }
        if i == 0 {
            out.push_str(segment);
        } else {
            let mut chars = segment.chars();
            if let Some(c) = chars.next() {
                out.extend(c.to_uppercase());
                out.extend(chars);
            }
        }
    }
    out
}

/// `console-io` → `ConsoleIo`
pub fn to_pascal_case(kebab: &str) -> String {
    let camel = to_camel_case(kebab);
    let mut chars = camel.chars();
    match chars.next() {
        None => String::new(),
        Some(c) => {
            let mut s = String::new();
            s.extend(c.to_uppercase());
            s.extend(chars);
            s
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn camel_case_examples() {
        assert_eq!(to_camel_case("console-io"), "consoleIo");
        assert_eq!(to_camel_case("read-line"), "readLine");
        assert_eq!(to_camel_case("info"), "info");
    }

    #[test]
    fn pascal_case_examples() {
        assert_eq!(to_pascal_case("console-io"), "ConsoleIo");
        assert_eq!(to_pascal_case("environment"), "Environment");
    }
}
