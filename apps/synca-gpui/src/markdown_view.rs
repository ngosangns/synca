//! A skill file drawn as markdown the pointer can select across blocks.
//!
//! The file is a stateful [`MarkdownView`] entity, so the selection survives
//! repaints of everything around it. Structure (headings, quotes, lists,
//! tables, code fences) is rebuilt each render from [`Frame`]s; the selectable
//! runs live in `blocks` and keep their own size and frame. Blocks share one
//! selection, so a drag that leaves a block keeps going through the ones after
//! it. Bold, italic, strike, code, and links stay highlights. A link opens
//! when the press did not drag.

use std::cell::RefCell;
use std::ops::Range;
use std::rc::Rc;

use ely_gpui_component::theme::{ActiveTheme, Palette};
use gpui::{
    App, ClipboardItem, Context, CursorStyle, DispatchPhase, Entity, FocusHandle, FontStyle,
    FontWeight, HighlightStyle, IntoElement, KeyDownEvent, MouseButton, MouseDownEvent,
    MouseMoveEvent, MouseUpEvent, ParentElement, Pixels, Point, Render, StrikethroughStyle, Styled,
    StyledText, TextLayout, UnderlineStyle, Window, canvas, div, prelude::*, px,
};
use markdown::mdast::{self, Node};

use crate::theme;

/// The markdown under `source` as an entity. The caller caches it per file so
/// unrelated renders keep the selection the pointer already made.
pub(crate) fn markdown_entity(source: String, cx: &mut App) -> Entity<MarkdownView> {
    cx.new(|cx| MarkdownView::new(source, cx.focus_handle()))
}

/// One drawn run of inline content: its text, the inline marks over it, and
/// the links inside it. Selections and copies work on these runs.
struct Block {
    text: String,
    styles: Vec<(Range<usize>, Mark)>,
    links: Vec<(Range<usize>, String)>,
}

#[derive(Clone, Copy, PartialEq, Debug)]
enum Mark {
    Strong,
    Emphasis,
    Strike,
    Code,
    Link,
}

/// Where a byte sits: which block, then where inside it.
#[derive(Clone, Copy, PartialEq)]
struct Span {
    block: usize,
    at: usize,
}

impl Span {
    fn new(block: usize, at: usize) -> Self {
        Self { block, at }
    }
}

/// The element tree of one file: structure here, selectable runs in `blocks`.
enum Frame {
    Heading {
        index: usize,
        level: u8,
    },
    Paragraph {
        index: usize,
    },
    Code {
        index: usize,
        lang: Option<String>,
    },
    Quote(Vec<Frame>),
    List {
        ordered: bool,
        start: Option<u32>,
        items: Vec<ListItem>,
    },
    Table {
        rows: Vec<Vec<usize>>,
    },
    Rule,
    Text {
        index: usize,
    },
    Empty,
}

struct ListItem {
    checked: Option<bool>,
    frames: Vec<Frame>,
}

pub(crate) struct MarkdownView {
    frames: Vec<Frame>,
    blocks: Vec<Block>,
    /// Each block's painted text layout, recorded during paint; hit tests read
    /// the window-space bounds the layout learned then.
    rects: Rc<RefCell<Vec<Option<TextLayout>>>>,
    anchor: Span,
    head: Span,
    dragging: bool,
    /// The pointer moved since the press, so the release is not a link click.
    moved: bool,
    pressed: Option<Span>,
    focus_handle: FocusHandle,
}

impl MarkdownView {
    fn new(source: String, focus_handle: FocusHandle) -> Self {
        let options = markdown::ParseOptions {
            constructs: markdown::Constructs::gfm(),
            ..markdown::ParseOptions::default()
        };
        let root = markdown::to_mdast(&source, &options).unwrap_or(Node::Root(mdast::Root {
            children: Vec::new(),
            position: None,
        }));
        let children = root.children().map(Vec::as_slice).unwrap_or(&[]);
        let mut blocks = Vec::new();
        let frames = build_frames(children, &mut blocks);
        let rects = Rc::new(RefCell::new(vec![None; blocks.len()]));
        Self {
            frames,
            blocks,
            rects,
            anchor: Span::new(0, 0),
            head: Span::new(0, 0),
            dragging: false,
            moved: false,
            pressed: None,
            focus_handle,
        }
    }

    /// One selectable run: the block text with its inline marks plus the
    /// selection highlight, plus a paint hook that records the painted layout
    /// for the shared hit tests. Press handling lives on the root so drags can
    /// also start in the gaps between blocks.
    fn text_element(
        &self,
        index: usize,
        colors: &Palette,
        rule: Pixels,
        cx: &Context<Self>,
    ) -> gpui::AnyElement {
        let block = &self.blocks[index];
        let mut highlights = inline_highlights(block, colors, rule);
        if let Some(range) = covered(self.anchor, self.head, index, block.text.len()) {
            highlights.push((
                range,
                HighlightStyle {
                    background_color: Some(cx.theme().colors.selection),
                    ..HighlightStyle::default()
                },
            ));
        }
        let styled = StyledText::new(block.text.clone()).with_highlights(split(highlights));
        let layout = styled.layout().clone();
        let rects = self.rects.clone();
        div()
            .cursor(CursorStyle::IBeam)
            .relative()
            .child(styled)
            .child(
                canvas(
                    |_, _, _| (),
                    move |_, _, _, _| {
                        rects.borrow_mut()[index] = Some(layout.clone());
                    },
                )
                .absolute()
                .inset_0(),
            )
            .into_any_element()
    }
}

impl Render for MarkdownView {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let colors = cx.theme().colors.clone();
        let rule = cx.theme().underline_thickness();
        let entity = cx.entity().downgrade();
        // Listeners registered outside paint are cleared every frame, so the
        // move/up handlers re-register from this canvas on every paint and
        // check `dragging` inside. They see every event in the window, not
        // just the ones over a block, and stay live for a drag that starts
        // after the last paint (synthetic input sends down and moves in one
        // burst with no frame between).
        div()
            .id("markdown-view")
            .w_full()
            .flex()
            .flex_col()
            .gap_3()
            .text_color(theme().text)
            .track_focus(&self.focus_handle)
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|this, event: &MouseDownEvent, window, cx| {
                    let Some(span) = span_at(&this.rects, event.position) else {
                        return;
                    };
                    window.focus(&this.focus_handle, cx);
                    this.pressed = Some(span);
                    this.moved = false;
                    this.dragging = true;
                    if !event.modifiers.shift {
                        this.anchor = span;
                    }
                    this.head = span;
                    cx.notify();
                }),
            )
            .on_key_down(cx.listener(|this, event: &KeyDownEvent, _, cx| {
                let stroke = &event.keystroke;
                match (stroke.modifiers.secondary(), stroke.key.as_str()) {
                    (true, "c") => {
                        let taken = selection_text(&this.blocks, this.anchor, this.head);
                        if !taken.is_empty() {
                            cx.write_to_clipboard(ClipboardItem::new_string(taken));
                        }
                    }
                    (true, "a") => {
                        this.anchor = Span::new(0, 0);
                        this.head = Span::new(usize::MAX, usize::MAX);
                        cx.notify();
                    }
                    (false, "escape") if this.anchor != this.head => {
                        this.anchor = this.head;
                        cx.notify();
                    }
                    _ => {}
                }
            }))
            .children(draw_frames(&self.frames, 0, self, &colors, rule, cx))
            .child(canvas(
                |_, _, _| (),
                move |_, _, window, _| {
                    let mover = entity.clone();
                    let finisher = entity;
                    window.on_mouse_event(
                        move |event: &MouseMoveEvent, phase: DispatchPhase, _, cx| {
                            if phase != DispatchPhase::Bubble || !event.dragging() {
                                return;
                            }
                            let _ = mover.update(cx, |this, cx| {
                                if !this.dragging {
                                    return;
                                }
                                if let Some(span) = span_at(&this.rects, event.position)
                                    && this.head != span
                                {
                                    this.moved = true;
                                    this.head = span;
                                    cx.notify();
                                }
                            });
                        },
                    );
                    window.on_mouse_event(
                        move |event: &MouseUpEvent, phase: DispatchPhase, _, cx| {
                            if phase != DispatchPhase::Bubble {
                                return;
                            }
                            let _ = finisher.update(cx, |this, cx| {
                                if !this.dragging {
                                    return;
                                }
                                this.dragging = false;
                                this.moved = false;
                                let pressed = this.pressed.take();
                                let release = span_at(&this.rects, event.position);
                                if let Some(press) = pressed
                                    && release == Some(press)
                                    && let Some(block) = this.blocks.get(press.block)
                                    && let Some((_, url)) = block
                                        .links
                                        .iter()
                                        .find(|(range, _)| range.contains(&press.at))
                                    && (url.starts_with("http://") || url.starts_with("https://"))
                                {
                                    ns_ui::open_url(url);
                                }
                                cx.notify();
                            });
                        },
                    );
                },
            ))
    }
}

/// Walks the frames into elements, mirroring how [`build_frames`] made them so
/// each leaf reads the block it was built with.
fn draw_frames(
    frames: &[Frame],
    depth: u32,
    view: &MarkdownView,
    colors: &Palette,
    rule: Pixels,
    cx: &Context<MarkdownView>,
) -> Vec<gpui::AnyElement> {
    frames
        .iter()
        .map(|frame| draw_frame(frame, depth, view, colors, rule, cx))
        .collect()
}

fn draw_frame(
    frame: &Frame,
    depth: u32,
    view: &MarkdownView,
    colors: &Palette,
    rule: Pixels,
    cx: &Context<MarkdownView>,
) -> gpui::AnyElement {
    match frame {
        Frame::Heading { index, level } => {
            let size = match level {
                1 => 22.,
                2 => 18.,
                3 => 16.,
                _ => 14.,
            };
            div()
                .pt_2()
                .text_size(px(size))
                .font_weight(FontWeight::BOLD)
                .line_height(px(size * 1.3))
                .text_color(if *level == 6 {
                    colors.fg_muted
                } else {
                    colors.fg
                })
                .child(view.text_element(*index, colors, rule, cx))
                .into_any_element()
        }
        Frame::Paragraph { index } => div()
            .text_color(colors.fg)
            .child(view.text_element(*index, colors, rule, cx))
            .into_any_element(),
        Frame::Quote(frames) => div()
            .flex()
            .flex_col()
            .gap_2()
            .pl_4()
            .border_l_2()
            .border_color(colors.border_strong)
            .text_color(colors.fg_muted)
            .children(draw_frames(frames, depth, view, colors, rule, cx))
            .into_any_element(),
        Frame::List {
            ordered,
            start,
            items,
        } => {
            let start = start.unwrap_or(1);
            let mut rows = Vec::new();
            for (index, item) in items.iter().enumerate() {
                let marker = match item.checked {
                    Some(true) => "☑".to_string(),
                    Some(false) => "☐".to_string(),
                    None if *ordered => format!("{}.", u64::from(start) + index as u64),
                    None => "•".to_string(),
                };
                rows.push(
                    div()
                        .flex()
                        .items_start()
                        .gap_2()
                        .pl(px(depth as f32 * 16.))
                        .child(
                            div()
                                .w(px(22.))
                                .flex_shrink_0()
                                .text_color(colors.fg_muted)
                                .child(marker),
                        )
                        .child(
                            div()
                                .flex_1()
                                .min_w(px(0.))
                                .flex()
                                .flex_col()
                                .gap_1()
                                .children(draw_frames(
                                    &item.frames,
                                    depth + 1,
                                    view,
                                    colors,
                                    rule,
                                    cx,
                                )),
                        )
                        .into_any_element(),
                );
            }
            div()
                .flex()
                .flex_col()
                .gap_1()
                .children(rows)
                .into_any_element()
        }
        Frame::Code { index, lang } => div()
            .flex()
            .flex_col()
            .gap_1()
            .p_3()
            .rounded_md()
            .bg(colors.sunken)
            .text_size(px(12.))
            .text_color(colors.syntax.variable)
            .children(lang.as_ref().map(|name| {
                div()
                    .text_size(px(11.))
                    .text_color(colors.fg_subtle)
                    .child(name.clone())
            }))
            .child(view.text_element(*index, colors, rule, cx))
            .into_any_element(),
        Frame::Rule => div()
            .my_2()
            .border_t_1()
            .border_color(colors.border_strong)
            .into_any_element(),
        Frame::Table { rows } => {
            let mut table_rows = Vec::new();
            for (row_index, cells) in rows.iter().enumerate() {
                let mut row = Vec::new();
                for &index in cells {
                    row.push(
                        div()
                            .flex_1()
                            .min_w(px(0.))
                            .p_2()
                            .when(row_index == 0, |cell| {
                                cell.font_weight(FontWeight::SEMIBOLD)
                            })
                            .child(view.text_element(index, colors, rule, cx))
                            .into_any_element(),
                    );
                }
                table_rows.push(
                    div()
                        .flex()
                        .when(row_index + 1 != rows.len(), |row| {
                            row.border_b_1().border_color(colors.border)
                        })
                        .children(row)
                        .into_any_element(),
                );
            }
            div()
                .rounded_md()
                .border_1()
                .border_color(colors.border)
                .overflow_hidden()
                .flex()
                .flex_col()
                .children(table_rows)
                .into_any_element()
        }
        Frame::Text { index } => div()
            .text_color(colors.fg)
            .child(view.text_element(*index, colors, rule, cx))
            .into_any_element(),
        Frame::Empty => div().into_any_element(),
    }
}

/// Parses the file into frames and the runs they draw, in draw order.
fn build_frames(nodes: &[Node], blocks: &mut Vec<Block>) -> Vec<Frame> {
    nodes.iter().map(|node| build_frame(node, blocks)).collect()
}

fn build_frame(node: &Node, blocks: &mut Vec<Block>) -> Frame {
    match node {
        Node::Heading(heading) => {
            let index = push_block(blocks, flatten(&heading.children));
            Frame::Heading {
                index,
                level: heading.depth.min(6),
            }
        }
        Node::Paragraph(paragraph) => {
            let index = push_block(blocks, flatten(&paragraph.children));
            Frame::Paragraph { index }
        }
        Node::Blockquote(quote) => Frame::Quote(build_frames(&quote.children, blocks)),
        Node::List(list) => Frame::List {
            ordered: list.ordered,
            start: list.start,
            items: list
                .children
                .iter()
                .filter_map(|item| match item {
                    Node::ListItem(item) => Some(ListItem {
                        checked: item.checked,
                        frames: build_frames(&item.children, blocks),
                    }),
                    _ => None,
                })
                .collect(),
        },
        Node::Code(code) => {
            let index = push_block(
                blocks,
                Block {
                    text: code.value.trim_end_matches('\n').to_string(),
                    styles: Vec::new(),
                    links: Vec::new(),
                },
            );
            Frame::Code {
                index,
                lang: code.lang.clone(),
            }
        }
        Node::ThematicBreak(_) => Frame::Rule,
        Node::Table(table) => Frame::Table {
            rows: table
                .children
                .iter()
                .filter_map(|row| match row {
                    Node::TableRow(row) => Some(
                        row.children
                            .iter()
                            .filter_map(|cell| match cell {
                                Node::TableCell(cell) => {
                                    Some(push_block(blocks, flatten(&cell.children)))
                                }
                                _ => None,
                            })
                            .collect(),
                    ),
                    _ => None,
                })
                .collect(),
        },
        other => {
            let run = flatten(other.children().map(Vec::as_slice).unwrap_or(&[]));
            if run.text.is_empty() {
                Frame::Empty
            } else {
                Frame::Text {
                    index: push_block(blocks, run),
                }
            }
        }
    }
}

fn push_block(blocks: &mut Vec<Block>, block: Block) -> usize {
    blocks.push(block);
    blocks.len() - 1
}

/// The bytes the selection covers in block `index`, and whether it does.
fn covered(anchor: Span, head: Span, index: usize, len: usize) -> Option<Range<usize>> {
    let (from, to) = ordered(anchor, head);
    if index < from.block || index > to.block {
        return None;
    }
    let start = if index == from.block {
        from.at.min(len)
    } else {
        0
    };
    let end = if index == to.block {
        to.at.min(len)
    } else {
        len
    };
    (start < end).then_some(start..end)
}

fn ordered(anchor: Span, head: Span) -> (Span, Span) {
    if (anchor.block, anchor.at) <= (head.block, head.at) {
        (anchor, head)
    } else {
        (head, anchor)
    }
}

/// The selected text, blocks joined the way they were drawn. A `usize::MAX`
/// head (⌘A) covers every block.
fn selection_text(blocks: &[Block], anchor: Span, head: Span) -> String {
    let (from, to) = ordered(anchor, head);
    let mut out = String::new();
    for (index, block) in blocks.iter().enumerate() {
        if index < from.block || index > to.block {
            continue;
        }
        let text = block.text.as_str();
        let start = if index == from.block {
            from.at.min(text.len())
        } else {
            0
        };
        let end = if index == to.block {
            to.at.min(text.len())
        } else {
            text.len()
        };
        if start >= end {
            continue;
        }
        if !out.is_empty() {
            out.push('\n');
        }
        out.push_str(text.get(start..end).unwrap_or(""));
    }
    out
}

/// The block under the window point: the last one that starts above it, or,
/// among blocks on the same row (table cells), the last one that starts to its
/// left. The point clamps to the first and last block and to each block's box.
fn span_at(rects: &RefCell<Vec<Option<TextLayout>>>, position: Point<Pixels>) -> Option<Span> {
    let rects = rects.borrow();
    let laid: Vec<(usize, &TextLayout)> = rects
        .iter()
        .enumerate()
        .filter_map(|(index, slot)| slot.as_ref().map(|layout| (index, layout)))
        .collect();
    let &(first, _) = laid.first()?;
    let mut chosen = first;
    for &(index, layout) in &laid {
        let bounds = layout.bounds();
        if bounds.origin.y < position.y
            || (bounds.origin.y == position.y && bounds.origin.x <= position.x)
        {
            chosen = index;
        } else {
            break;
        }
    }
    let layout = &laid.iter().find(|(index, _)| *index == chosen)?.1;
    let clamped = gpui::point(position.x, position.y.min(layout.bounds().bottom()));
    Some(Span::new(chosen, index_at(layout, clamped)))
}

fn index_at(layout: &TextLayout, position: Point<Pixels>) -> usize {
    match layout.index_for_position(position) {
        Ok(ix) | Err(ix) => ix,
    }
}

/// The inline marks of a block as paint styles.
fn inline_highlights(
    block: &Block,
    colors: &Palette,
    rule: Pixels,
) -> Vec<(Range<usize>, HighlightStyle)> {
    block
        .styles
        .iter()
        .map(|(range, mark)| {
            let style = match mark {
                Mark::Strong => HighlightStyle {
                    font_weight: Some(FontWeight::BOLD),
                    ..HighlightStyle::default()
                },
                Mark::Emphasis => HighlightStyle {
                    font_style: Some(FontStyle::Italic),
                    ..HighlightStyle::default()
                },
                Mark::Strike => HighlightStyle {
                    strikethrough: Some(StrikethroughStyle {
                        thickness: rule,
                        color: Some(colors.fg_muted),
                    }),
                    ..HighlightStyle::default()
                },
                Mark::Code => HighlightStyle {
                    background_color: Some(colors.sunken),
                    ..HighlightStyle::default()
                },
                Mark::Link => HighlightStyle {
                    color: Some(colors.link),
                    underline: Some(UnderlineStyle {
                        thickness: rule,
                        color: Some(colors.link.opacity(0.4)),
                        wavy: false,
                    }),
                    ..HighlightStyle::default()
                },
            };
            (range.clone(), style)
        })
        .collect()
}

/// Overlapping ranges cut into the pieces `StyledText` can paint, later styles
/// covering earlier ones where they share bytes.
fn split(spans: Vec<(Range<usize>, HighlightStyle)>) -> Vec<(Range<usize>, HighlightStyle)> {
    let mut edges: Vec<usize> = spans
        .iter()
        .flat_map(|(range, _)| [range.start, range.end])
        .collect();
    edges.sort_unstable();
    edges.dedup();
    edges
        .windows(2)
        .filter_map(|pair| {
            let (start, end) = (pair[0], pair[1]);
            spans
                .iter()
                .filter(|(range, _)| range.start <= start && end <= range.end)
                .fold(None, |below: Option<HighlightStyle>, (_, style)| {
                    Some(below.map_or(*style, |below| below.highlight(*style)))
                })
                .map(|style| (start..end, style))
        })
        .collect()
}

fn flatten(nodes: &[Node]) -> Block {
    let mut block = Block {
        text: String::new(),
        styles: Vec::new(),
        links: Vec::new(),
    };
    for node in nodes {
        push(node, &mut block);
    }
    block
}

fn push(node: &Node, block: &mut Block) {
    match node {
        Node::Text(text) => block.text.push_str(&text.value),
        Node::InlineCode(code) => mark(&code.value, Mark::Code, block),
        Node::Strong(strong) => wrap(&strong.children, Mark::Strong, block),
        Node::Emphasis(emphasis) => wrap(&emphasis.children, Mark::Emphasis, block),
        Node::Delete(delete) => wrap(&delete.children, Mark::Strike, block),
        Node::Link(link) => {
            let start = block.text.len();
            for child in &link.children {
                push(child, block);
            }
            let end = block.text.len();
            if start < end {
                block.styles.push((start..end, Mark::Link));
                block.links.push((start..end, link.url.clone()));
            }
        }
        Node::Break(_) => block.text.push('\n'),
        other => {
            if let Some(children) = other.children() {
                for child in children {
                    push(child, block);
                }
            }
        }
    }
}

fn mark(text: &str, style: Mark, block: &mut Block) {
    if text.is_empty() {
        return;
    }
    let start = block.text.len();
    block.text.push_str(text);
    block.styles.push((start..block.text.len(), style));
}

fn wrap(children: &[Node], style: Mark, block: &mut Block) {
    let start = block.text.len();
    for child in children {
        push(child, block);
    }
    let end = block.text.len();
    if start < end {
        block.styles.push((start..end, style));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn blocks(texts: &[&str]) -> Vec<Block> {
        texts
            .iter()
            .map(|text| Block {
                text: (*text).to_string(),
                styles: Vec::new(),
                links: Vec::new(),
            })
            .collect()
    }

    #[test]
    fn a_drag_across_blocks_joins_their_text() {
        let doc = blocks(&["first line", "second block", "third"]);
        let taken = selection_text(&doc, Span::new(0, 6), Span::new(2, 5));
        assert_eq!(taken, "line\nsecond block\nthird");
    }

    #[test]
    fn a_reversed_drag_covers_the_same_bytes() {
        let doc = blocks(&["first line", "second block"]);
        assert_eq!(
            selection_text(&doc, Span::new(1, 6), Span::new(0, 6)),
            selection_text(&doc, Span::new(0, 6), Span::new(1, 6)),
        );
    }

    #[test]
    fn select_all_takes_every_block() {
        let doc = blocks(&["one", "two", "three"]);
        let taken = selection_text(&doc, Span::new(0, 0), Span::new(usize::MAX, usize::MAX));
        assert_eq!(taken, "one\ntwo\nthree");
    }

    #[test]
    fn an_empty_selection_copies_nothing() {
        let doc = blocks(&["one", "two"]);
        assert_eq!(selection_text(&doc, Span::new(1, 2), Span::new(1, 2)), "");
    }

    #[test]
    fn covered_ranges_split_at_block_edges() {
        let (anchor, head) = (Span::new(0, 3), Span::new(2, 4));
        assert_eq!(covered(anchor, head, 0, 8), Some(3..8));
        assert_eq!(covered(anchor, head, 1, 8), Some(0..8));
        assert_eq!(covered(anchor, head, 2, 8), Some(0..4));
        assert_eq!(covered(anchor, head, 3, 8), None);
    }

    #[test]
    fn a_collapsed_selection_covers_nothing() {
        assert_eq!(covered(Span::new(1, 4), Span::new(1, 4), 1, 8), None);
    }

    #[test]
    fn overlapping_marks_cut_into_ordered_pieces() {
        let bold = HighlightStyle {
            font_weight: Some(FontWeight::BOLD),
            ..HighlightStyle::default()
        };
        let italic = HighlightStyle {
            font_style: Some(FontStyle::Italic),
            ..HighlightStyle::default()
        };
        let pieces = split(vec![(0..6, bold), (3..9, italic)]);
        assert_eq!(
            pieces
                .iter()
                .map(|(range, _)| range.clone())
                .collect::<Vec<_>>(),
            [0..3, 3..6, 6..9]
        );
    }

    #[test]
    fn parsing_turns_structure_into_blocks_in_draw_order() {
        let mut doc = Vec::new();
        let frames = build_frames(
            &parse("# Title\n\nHello **world**\n\n```rust\nlet x = 1;\n```\n"),
            &mut doc,
        );
        let texts: Vec<&str> = doc.iter().map(|block| block.text.as_str()).collect();
        assert_eq!(texts, ["Title", "Hello world", "let x = 1;"]);
        assert_eq!(doc[1].styles, [(6..11, Mark::Strong)]);
        assert!(matches!(
            frames.as_slice(),
            [
                Frame::Heading { .. },
                Frame::Paragraph { .. },
                Frame::Code { .. }
            ]
        ));
    }

    #[test]
    fn links_keep_their_url_over_the_label() {
        let mut doc = Vec::new();
        build_frames(
            &parse("see [the guide](https://example.com) here"),
            &mut doc,
        );
        assert_eq!(doc[0].text, "see the guide here");
        assert_eq!(doc[0].links, [(4..13, "https://example.com".to_string())]);
    }

    fn parse(source: &str) -> Vec<Node> {
        let options = markdown::ParseOptions {
            constructs: markdown::Constructs::gfm(),
            ..markdown::ParseOptions::default()
        };
        markdown::to_mdast(source, &options)
            .unwrap()
            .children()
            .map(|children| children.clone())
            .unwrap_or_default()
    }
}
