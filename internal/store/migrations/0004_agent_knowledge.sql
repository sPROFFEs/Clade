-- Agent knowledge base mode: '' (none), 'raw' (a folder of documents
-- the agent reads with its own file tools), 'rag' (same folder plus a
-- graphify knowledge-graph index in knowledge/.graphify). The files
-- themselves live OUTSIDE the DB under
-- <config>/praimate/agents/<id>/knowledge/ — only the mode is a row
-- attribute, so switching formats never touches the data.
ALTER TABLE agents ADD COLUMN knowledge TEXT NOT NULL DEFAULT '';
