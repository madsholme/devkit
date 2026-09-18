FROM python:3.13-slim
ARG DEVBOX_COMMIT
ENV DEVBOX_COMMIT=$DEVBOX_COMMIT
WORKDIR /app
COPY app.py message.txt ./
EXPOSE 8000
CMD ["python", "app.py"]
