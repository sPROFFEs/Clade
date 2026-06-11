# Uploaded prompt files replace prompt image attachments

PrAImate GUI prompts are text-only: files entering the Project-connected Prompt are uploaded to PrAImate GUI Backend temp storage and inserted as `@<path>` mentions in the prompt text. We removed the parallel prompt image attachment channel because temp file mentions work consistently across Desktop, Web, Mobile, local backends, and hosted backends, and they support every file type instead of treating images as special.
