package wire

func ChatdChildren(node ChatdNode) []ChatdNode {
	children, ok := node.Content.([]ChatdNode)
	if !ok {
		return nil
	}
	return children
}
