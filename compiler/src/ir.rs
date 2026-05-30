//! Internal IR — emitters target this, not WIT types directly.

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Package {
    pub interfaces: Vec<Interface>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Interface {
    /// Emitted type name (PascalCase), e.g. `ConsoleIo`.
    pub name: String,
    pub functions: Vec<Function>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Function {
    /// Emitted method name (camelCase), e.g. `readLine`.
    pub name: String,
    pub params: Vec<Param>,
    pub result: Option<TypeRef>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Param {
    pub name: String,
    pub ty: TypeRef,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TypeRef {
    String,
    Option(Box<TypeRef>),
    Interface(String),
}
