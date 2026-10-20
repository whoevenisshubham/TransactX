export function firstName(name?: string) { return name?.trim().split(" ")[0] || "there"; }
