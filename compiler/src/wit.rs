use std::path::Path;

use anyhow::{Context, Result};
use wit_parser::{Resolve, Type, TypeDefKind, TypeOwner, WorldItem, WorldKey};

use crate::ir::{Function, Interface, Package, Param, TypeRef};
use crate::naming::{to_camel_case, to_pascal_case};

/// Parse a WIT directory into the ILC IR.
pub fn load_package(wit_dir: &Path) -> Result<Package> {
    let mut resolve = Resolve::default();
    resolve
        .push_dir(wit_dir)
        .with_context(|| format!("failed to parse WIT in {}", wit_dir.display()))?;

    let mut interfaces = Vec::new();

    for (_id, iface) in resolve.interfaces.iter() {
        let iface_name = match iface.name.as_deref() {
            Some(name) => name,
            None => continue,
        };

        let functions = iface
            .functions
            .iter()
            .map(|(_name, func)| {
                let params = func
                    .params
                    .iter()
                    .map(|(name, ty)| Param {
                        name: name.clone(),
                        ty: wit_type_to_ir(&resolve, *ty),
                    })
                    .collect();

                let result = func
                    .results
                    .iter_types()
                    .next()
                    .map(|ty| wit_type_to_ir(&resolve, *ty));

                Function {
                    name: to_camel_case(&func.name),
                    params,
                    result,
                }
            })
            .collect();

        interfaces.push(Interface {
            name: to_pascal_case(iface_name),
            functions,
        });
    }

    for (_id, world) in resolve.worlds.iter() {
        let functions: Vec<Function> = world
            .imports
            .iter()
            .filter_map(|(key, item)| world_import_to_function(&resolve, key, item))
            .collect();

        if functions.is_empty() {
            continue;
        }

        interfaces.push(Interface {
            name: to_pascal_case(&world.name),
            functions,
        });
    }

    interfaces.sort_by(|a, b| a.name.cmp(&b.name));

    Ok(Package { interfaces })
}

fn wit_type_to_ir(resolve: &Resolve, ty: wit_parser::Type) -> TypeRef {
    match ty {
        Type::String => TypeRef::String,
        Type::Id(id) => {
            let def = &resolve.types[id];
            match &def.kind {
                TypeDefKind::Option(inner) => {
                    TypeRef::Option(Box::new(wit_type_to_ir(resolve, *inner)))
                }
                TypeDefKind::Type(inner) => wit_type_to_ir(resolve, *inner),
                _ => interface_name_from_type(resolve, id).unwrap_or(TypeRef::String),
            }
        }
        _ => TypeRef::String,
    }
}

fn world_import_to_function(
    resolve: &Resolve,
    key: &WorldKey,
    item: &WorldItem,
) -> Option<Function> {
    let WorldItem::Interface { id, .. } = item else {
        return None;
    };
    let iface = &resolve.interfaces[*id];
    let iface_name = iface.name.as_deref()?;
    let import_name = match key {
        WorldKey::Name(name) => name.as_str(),
        WorldKey::Interface(_) => iface_name,
    };
    Some(Function {
        name: to_camel_case(import_name),
        params: vec![],
        result: Some(TypeRef::Interface(to_pascal_case(iface_name))),
    })
}

fn interface_name_from_type(resolve: &Resolve, id: wit_parser::TypeId) -> Option<TypeRef> {
    let def = &resolve.types[id];
    if let TypeOwner::Interface(iface_id) = def.owner {
        let iface = &resolve.interfaces[iface_id];
        let name = iface.name.as_deref().or(def.name.as_deref())?;
        return Some(TypeRef::Interface(to_pascal_case(name)));
    }
    def.name
        .as_deref()
        .map(|n| TypeRef::Interface(to_pascal_case(n)))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn loads_phase1_wit() {
        let wit = Path::new(env!("CARGO_MANIFEST_DIR")).join("../wit");
        let pkg = load_package(&wit).expect("parse wit");
        assert_eq!(pkg.interfaces.len(), 2);
        let console = pkg
            .interfaces
            .iter()
            .find(|i| i.name == "ConsoleIo")
            .expect("ConsoleIo");
        assert!(console.functions.iter().any(|f| f.name == "readLine"));
        let env = pkg
            .interfaces
            .iter()
            .find(|i| i.name == "Environment")
            .expect("Environment");
        assert!(env.functions.iter().any(|f| f.name == "consoleIo"));
    }
}
