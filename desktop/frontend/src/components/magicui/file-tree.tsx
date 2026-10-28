// Adapted from Magic UI's File Tree registry component (MIT).
// Source: https://magicui.design/docs/components/file-tree
// Tree context, Folder/File composition and directory indicators are retained.
// Flat rows let the workspace own lazy loading and virtualization rather than
// nesting accordion scroll regions or duplicating its selection state.
import { createContext, forwardRef, useContext, useMemo } from "react";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { FileText, Folder as FolderIcon, FolderOpen } from "../SemantixIcons";

type TreeContextProps = {
  selectedId: string | null;
  expandedItems: ReadonlySet<string>;
  indicator: boolean;
};
const TreeContext = createContext<TreeContextProps | null>(null);
function useTree() {
  const context = useContext(TreeContext);
  if (!context) throw new Error("useTree must be used within a TreeProvider");
  return context;
}

const Tree = forwardRef<HTMLDivElement, ComponentPropsWithoutRef<"div"> & {
  selectedId: string | null;
  expandedItems: ReadonlySet<string>;
  indicator?: boolean;
}>(({ selectedId, expandedItems, indicator = true, className = "", children, ...props }, ref) => {
  const context = useMemo(() => ({ selectedId, expandedItems, indicator }), [selectedId, expandedItems, indicator]);
  return <TreeContext.Provider value={context}>
    <div {...props} ref={ref} role="tree" className={`magic-file-tree ${className}`}>{children}</div>
  </TreeContext.Provider>;
});
Tree.displayName = "Tree";

function TreeIndicator({ depth }: { depth: number }) {
  const { indicator } = useTree();
  if (!indicator || depth < 1) return null;
  return <span className="magic-file-tree__indicators" aria-hidden="true">
    {Array.from({ length: depth }, (_, index) => <span key={index} className="magic-file-tree__indicator" style={{ left: 15 + index * 18 }} />)}
  </span>;
}

type RowProps = Omit<ComponentPropsWithoutRef<"button">, "value"> & {
  value: string;
  depth?: number;
  isSelectable?: boolean;
  isSelect?: boolean;
};
const Folder = forwardRef<HTMLButtonElement, RowProps & {
  element: ReactNode;
  isOpen?: boolean;
}>(({ value, depth = 0, element, isOpen, isSelect, isSelectable = true, className = "", style, onClick, onKeyDown, ...props }, ref) => {
  const { expandedItems, selectedId } = useTree();
  const expanded = isOpen ?? expandedItems.has(value);
  const selected = isSelect ?? selectedId === value;
  const Icon = expanded ? FolderOpen : FolderIcon;
  return <button {...props} ref={ref} type="button" role="treeitem" aria-level={depth + 1}
    aria-expanded={expanded} aria-selected={selected} disabled={!isSelectable}
    className={`magic-file-tree__item magic-file-tree__folder ${className}`}
    style={{ paddingInlineStart: 8 + depth * 18, ...style }}
    onClick={onClick} onKeyDown={(event) => {
      onKeyDown?.(event);
      if (event.defaultPrevented) return;
      if ((event.key === "ArrowRight" && !expanded) || (event.key === "ArrowLeft" && expanded)) {
        event.preventDefault(); event.currentTarget.click();
      }
    }}>
    <TreeIndicator depth={depth} />
    <Icon size={15} className="magic-file-tree__icon" aria-hidden="true" />
    {element}
  </button>;
});
Folder.displayName = "Folder";

const File = forwardRef<HTMLButtonElement, RowProps & {
  fileIcon?: ReactNode;
  handleSelect?: (id: string) => void;
}>(({ value, depth = 0, isSelect, isSelectable = true, fileIcon, handleSelect, onClick, className = "", style, children, ...props }, ref) => {
  const { selectedId } = useTree();
  const selected = isSelect ?? selectedId === value;
  return <button {...props} ref={ref} type="button" role="treeitem" aria-level={depth + 1}
    aria-selected={selected} disabled={!isSelectable}
    className={`magic-file-tree__item magic-file-tree__file ${className}`}
    style={{ paddingInlineStart: 8 + depth * 18, ...style }} onClick={(event) => {
      handleSelect?.(value); onClick?.(event);
    }}>
    <TreeIndicator depth={depth} />
    {fileIcon ?? <FileText size={15} className="magic-file-tree__icon" aria-hidden="true" />}
    {children}
  </button>;
});
File.displayName = "File";

// Magic UI default ordering: folders first, natural case-insensitive names.
const treeCollator = new Intl.Collator("en", { numeric: true, sensitivity: "base" });
export function compareTreeEntries(a: { name: string; isDir: boolean }, b: { name: string; isDir: boolean }) {
  return a.isDir !== b.isDir ? (a.isDir ? -1 : 1) : treeCollator.compare(a.name, b.name);
}
export { Tree, Folder, File };
